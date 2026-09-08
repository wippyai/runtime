# Native application host

This package provides the boot boundary used by standalone Wippy applications.
The application supplies a complete, versioned bundle of canonical Hub packs and
an explicit list of native boot components. The generated executable imports
`application.Run`; it does not copy CLI loaders or implement a Hub resolver.

`Bundle.Seed` validates pack hashes and published identities before creating a
canonical deployment lock and vendor directory. An existing lock selecting the
same root is preserved, even when it selects a newer application version. A
corrupt or unrelated deployment fails closed.

`Run` accepts `--state-dir`, `--command`, and `--base`, followed by `run` and
application arguments, `update`, or `runtime` and canonical Wippy CLI arguments.
With no explicit operation it runs the application's configured command. Default
state is under the OS user configuration directory and executable name. The
caller's working directory remains unchanged. Applications can map environment
variables to workspace data paths with `Options.DataEnv`; explicit environment
values take precedence.

`update` stages the selected deployment, calls the executable's canonical Wippy
update and lint commands, verifies installed pack digests, and switches the
activation record only after success. An exclusive process-lifetime state lock
prevents a concurrent launch or update. Previous deployment files remain available.
The advanced `runtime` command directly exposes Wippy operations; it does not
apply the standalone staged-update wrapper.

`base` mode exposes `--base` as an explicit recovery boot with an independent
registry history. `bootstrap` mode only seeds the initial deployment. Neither
mode deletes or rolls back application databases. An application's migration
checks still decide whether older bundled code can open newer data. There is no
claim of transparent rollback across incompatible data schemas.

Native components are additional host selections. Duplicate component names are
rejected, including attempts to replace built-ins. Their Lua declarations and
normal runtime permissions remain in force. The update lint gate detects missing
native module exports and type incompatibility; explicit semantic-version native
requirements are not implemented yet.

`cmd.ExecuteWithOptions` is the process-level adapter to the normal Wippy command
paths. Call it once from main, not concurrently or from application actors.
Host-selected registry paths override package settings so an application update
cannot relocate its deployment lock or registry history.

Run `make test-application` for the host, deployment and command regression tests.
