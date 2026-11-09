# Request: let a caller supply the credential store in `oci.NewOrasRemote`

- **Upstream**: [`zarf-dev/zarf`](https://github.com/zarf-dev/zarf)
- **Package**: `src/pkg/oci`
- **Read against**: v0.88.0, the release vendored here
- **Status**: landed upstream - PR [#5502](https://github.com/zarf-dev/zarf/pull/5502), commit [`be55273`](https://github.com/zarf-dev/zarf/commit/be55273), merged 2026-10-21. Not in v0.88.0.
- **Currency check**: `main` @ `2904d9e`, 2026-10-23 - answered, see below
- **Verified**: yes - prototyped against vendored v0.88.0, see *Verification* below

## This has landed upstream

`oci.WithCredentialFunc` is on `main`, in the shape this page proposed: a `Modifier` taking an `auth.CredentialFunc`, with the Docker config store consulted only when none was supplied. Upstream also took the ordering the *Verification* section below flagged - `setRepository` returns early once a credential func is set, so `NewStoreFromDocker` is never reached rather than being called and overwritten - which is what makes the modifier remove the dependency on `~/.docker/config.json` rather than merely override its result. `zoci.RemoteClientOptions.CredentialFunc` came with it, which this page did not ask for.

It is not in v0.88.0, which is what cargoship vendors, so the code quoted below is still what this tree compiles against. Nothing here needs filing; the rest of the page is kept as the record of the ask.

**Method note**: these pages are written against the release cargoship vendors, which is the right tree to read - it is the code we run, and the code a prototype can be measured against. What it cannot tell us is whether an ask has already been answered, which is how this page came to request something that existed. So the release stays the tree of record, and `main` gets one grep before filing, recorded in the *Currency check* line above. The same correction applies to [`zarf-oci-registry-rewrite.md`](zarf-oci-registry-rewrite.md).

## What was awkward in v0.88.0

`src/pkg/oci` is otherwise a well-behaved functional-options package: `NewOrasRemote` (`src/pkg/oci/common.go:115`) takes `...Modifier`, and `WithPlainHTTP`, `WithInsecureSkipVerify`, `WithTransport`, `WithUserAgent`, `WithLogger` and `WithCache` cover transport, TLS, logging and the layer cache without anybody needing to fork it.

Credentials are the one thing with no seam. `setRepository` reads them from the Docker config file unconditionally (`src/pkg/oci/common.go:168-176`):

```go
storeOpts := credentials.StoreOptions{}
credStore, err := credentials.NewStoreFromDocker(storeOpts)
if err != nil {
	return fmt.Errorf("failed to get credentials: %w", err)
}
client, ok := o.repo.Client.(*auth.Client)
if !ok {
	return fmt.Errorf("repository client is not an auth client")
}
client.Credential = credentials.Credential(credStore)
```

`setRepository` is unexported and is called from `NewOrasRemote`, so there is no point at which a caller can substitute a different source, and no way to opt out. A consumer that holds credentials somewhere other than `~/.docker/config.json` - a vault, an environment variable, a short-lived token minted per publish, or a config file of its own - has to either write the Docker config file out to disk first or fork the package.

It is also a hard failure rather than a soft one: with no Docker config present at all, `NewStoreFromDocker` is what decides whether construction succeeds, for a registry that may well need no authentication.

## Proposed change

One more modifier, in the style of the six that already exist:

```go
// WithCredentialFunc sets the credential source for the remote. When unset, credentials
// are read from the Docker config file as before.
func WithCredentialFunc(cred auth.CredentialFunc) Modifier
```

`auth.CredentialFunc` is already the type `credentials.Credential(credStore)` produces, so `setRepository` becomes:

```go
if o.credential != nil {
	client.Credential = o.credential
} else {
	// existing Docker config path
}
```

Default behaviour is unchanged for every current caller, and the Docker config store is still what you get for free.

## Verification

Prototyped against the vendored copy of v0.88.0 and then reverted. The patch is 58 lines in `src/pkg/oci/common.go`: one unexported `credential auth.CredentialFunc` field on `OrasRemote`, the `WithCredentialFunc` modifier as written above, and `setRepository` reordered so the `*auth.Client` assertion happens first and the Docker config store is consulted only when no credential func was supplied.

The ordering matters more than it looks. Done naively - keeping `NewStoreFromDocker` where it is and overwriting afterwards - the Docker config file is still read on every construction, so a consumer that supplied its own credentials still fails when there is no Docker config to read. Putting the store inside the `else` is what makes the modifier actually remove the dependency rather than just override its result.

A throwaway test confirmed the injected function is the one invoked, rather than merely compiling:

```go
cred := func(_ context.Context, host string) (auth.Credential, error) {
	called = true
	return auth.Credential{Username: "injected", Password: "pw"}, nil
}
r, err := oci.NewOrasRemote("ghcr.io/example/pkg:0.0.1", platform, oci.WithCredentialFunc(cred))
got, err := r.Repo().Client.(*auth.Client).Credential(ctx, "ghcr.io")
// called == true, got.Username == "injected"
```

The default path is unchanged: every other test in cargoship's suite constructs remotes without the modifier and passed, so the Docker config store is still what a caller gets for free.

## Why this is in upstream's interest

The Docker config file is a reasonable default for a CLI and a poor assumption for a library, and `src/pkg/oci` is now both - it is the package zarf asks library consumers to build on, having moved it in-tree from `github.com/defenseunicorns/pkg/oci`. This is the same class of change as [#5223](https://github.com/zarf-dev/zarf/issues/5223) ("Support configurable cluster connection for library users of Zarf") and [#4764](https://github.com/zarf-dev/zarf/issues/4764) (SDK cache-path resolution): a default that is correct for the binary, hardwired where a consumer needs to override it.

It would also let zarf's own tests exercise the auth path without a Docker config on the runner.

## Evidence that it is needed

Cargoship (`colonel-byte/cargoship`) runs from an air-gapped management node and publishes to a registry inside the enclave. It currently lives with the Docker config store, so this is not blocking us today - we are filing it because it is the one place in `src/pkg/oci` where adopting the package forces a fork rather than a modifier, and we would rather ask than discover that later.
