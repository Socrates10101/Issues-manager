// Command issues is both the HTTP server (`issues serve`) and the CLI client
// (`issues ls|show|new|status|archive`) for the issue tracker, plus a one-shot
// `issues migrate` to import the legacy dotfiles Issues/ tree.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/Socrates10101/issues/internal/api"
	"github.com/Socrates10101/issues/internal/client"
	"github.com/Socrates10101/issues/internal/migrate"
	"github.com/Socrates10101/issues/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	var err error
	switch cmd {
	case "serve":
		err = cmdServe(args)
	case "migrate":
		err = cmdMigrate(args)
	case "ls", "list":
		err = cmdList(args)
	case "show":
		err = cmdShow(args)
	case "new":
		err = cmdNew(args)
	case "status":
		err = cmdStatus(args)
	case "archive":
		err = cmdArchive(args)
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `issues — issue tracker (server + client)

Server:
  issues serve [-data DIR] [-addr HOST] [-port N] [-token-file FILE]
  issues migrate -from DIR [-data DIR]

Client (targets $ISSUES_URL, default http://127.0.0.1:8787):
  issues ls [-p PROJECT] [-s STATUS] [--archived] [--all]
  issues show <project>:<NNN>
  issues new "TITLE" -p PROJECT
  issues status <project>:<NNN> "<STATUS>"
  issues archive <project>:<NNN>
`)
}

func today() string { return time.Now().Format("2006-01-02") }

// ---- server commands ----

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	data := fs.String("data", envOr("ISSUES_DATA", "/var/lib/issues"), "data directory")
	addr := fs.String("addr", envOr("ISSUES_ADDR", "0.0.0.0"), "bind address")
	port := fs.Int("port", envOrInt("ISSUES_PORT", 8787), "listen port")
	tokenFile := fs.String("token-file", os.Getenv("ISSUES_TOKEN_FILE"), "file containing bearer token (optional)")
	_ = fs.Parse(args)

	st, err := store.New(*data, today)
	if err != nil {
		return err
	}
	token := ""
	if *tokenFile != "" {
		b, err := os.ReadFile(*tokenFile)
		if err != nil {
			return fmt.Errorf("read token file: %w", err)
		}
		token = strings.TrimSpace(string(b))
	}
	srv := &http.Server{
		Addr:    net.JoinHostPort(*addr, strconv.Itoa(*port)),
		Handler: api.New(st, token).Handler(),
	}
	go func() {
		fmt.Printf("issues serving on %s (data=%s, auth=%t)\n", srv.Addr, *data, token != "")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, "server error:", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

func cmdMigrate(args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ExitOnError)
	from := fs.String("from", "", "legacy Issues/ directory to import")
	data := fs.String("data", envOr("ISSUES_DATA", "/var/lib/issues"), "destination data directory")
	_ = fs.Parse(args)
	if *from == "" {
		return fmt.Errorf("-from is required")
	}
	st, err := store.New(*data, today)
	if err != nil {
		return err
	}
	res, err := migrate.Run(*from, st)
	if err != nil {
		return err
	}
	fmt.Printf("migrated %d active + %d archived = %d issues into %s\n",
		res.Active, res.Archived, res.Active+res.Archived, *data)
	return nil
}

// ---- client commands ----

func cmdList(args []string) error {
	fs := flag.NewFlagSet("ls", flag.ExitOnError)
	project := fs.String("p", "", "filter by project")
	status := fs.String("s", "", "filter by status")
	archived := fs.Bool("archived", false, "only archived issues")
	all := fs.Bool("all", false, "include archived issues")
	_ = fs.Parse(args)

	var arch *bool
	switch {
	case *archived:
		t := true
		arch = &t
	case !*all:
		fa := false
		arch = &fa
	}
	list, err := client.New().List(*project, *status, arch)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSTATUS\tA\tTITLE")
	for _, i := range list {
		a := ""
		if i.Archived {
			a = "✓"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", i.ID(), i.Status, a, i.Title)
	}
	tw.Flush()
	fmt.Printf("\n%d issues\n", len(list))
	return nil
}

func cmdShow(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: issues show <project>:<NNN>")
	}
	project, number, err := parseID(args[0])
	if err != nil {
		return err
	}
	i, err := client.New().Get(project, number)
	if err != nil {
		return err
	}
	fmt.Printf("# %s — %s\n", i.ID(), i.Title)
	fmt.Printf("status: %s\n", i.Status)
	if i.Created != "" {
		fmt.Printf("created: %s   updated: %s\n", i.Created, i.Updated)
	}
	if i.Archived {
		fmt.Println("archived: yes")
	}
	fmt.Println()
	fmt.Println(strings.TrimRight(i.Body, "\n"))
	return nil
}

func cmdNew(args []string) error {
	fs := flag.NewFlagSet("new", flag.ExitOnError)
	project := fs.String("p", "", "project (required)")
	// allow title before or after flags
	var title string
	rest := []string{}
	for _, a := range args {
		if strings.HasPrefix(a, "-") || (len(rest) > 0 && rest[len(rest)-1] == "-p") {
			rest = append(rest, a)
		} else if title == "" {
			title = a
		} else {
			rest = append(rest, a)
		}
	}
	_ = fs.Parse(rest)
	if title == "" || *project == "" {
		return fmt.Errorf("usage: issues new \"TITLE\" -p PROJECT")
	}
	i, err := client.New().Create(*project, title, "")
	if err != nil {
		return err
	}
	fmt.Printf("created %s — %s\n", i.ID(), i.Title)
	return nil
}

func cmdStatus(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: issues status <project>:<NNN> \"<STATUS>\"")
	}
	project, number, err := parseID(args[0])
	if err != nil {
		return err
	}
	i, err := client.New().SetStatus(project, number, args[1])
	if err != nil {
		return err
	}
	fmt.Printf("%s → %s\n", i.ID(), i.Status)
	return nil
}

func cmdArchive(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: issues archive <project>:<NNN>")
	}
	project, number, err := parseID(args[0])
	if err != nil {
		return err
	}
	i, err := client.New().Archive(project, number)
	if err != nil {
		return err
	}
	fmt.Printf("archived %s\n", i.ID())
	return nil
}

// parseID accepts "project:NNN".
func parseID(s string) (string, int, error) {
	p, n, ok := strings.Cut(s, ":")
	if !ok {
		return "", 0, fmt.Errorf("invalid id %q (want project:NNN)", s)
	}
	num, err := strconv.Atoi(strings.TrimSpace(n))
	if err != nil {
		return "", 0, fmt.Errorf("invalid number in %q", s)
	}
	return strings.TrimSpace(p), num, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envOrInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
