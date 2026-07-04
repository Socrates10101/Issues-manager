# buildGoModule package for the `issues` binary (server + client + migrate).
# Zero external Go dependencies, so vendorHash is null.
{ lib, buildGoModule }:

buildGoModule {
  pname = "issues";
  version = "0.1.0";

  src = lib.cleanSource ./..;

  vendorHash = null;

  # CGO is not needed; build a static-ish binary.
  env.CGO_ENABLED = "0";

  ldflags = [ "-s" "-w" ];

  meta = {
    description = "Lightweight issue tracker — HTTP CRUD service + CLI over markdown storage";
    mainProgram = "issues";
    platforms = lib.platforms.unix;
  };
}
