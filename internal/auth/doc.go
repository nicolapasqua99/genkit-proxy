// Package auth decouples gateway authentication from upstream provider
// credentials. A caller presents its own opaque gateway key; the Resolver
// authenticates that key against a configured tenant table and resolves the
// real provider API key from a SecretSource, instead of the key being passed
// straight through to the provider.
//
// # Layout
//
//   - resolver.go  Resolver and tenant table, gateway-key authentication, and
//     provider-key resolution; the configuration sentinels and parser.
//   - source.go    The SecretSource seam and its in-memory StaticSecretSource.
//
// The SecretSource seam keeps the lookup pluggable: the static, env-backed
// source ships today, and a Google Secret Manager source can replace it later
// without touching the tenant table or the Resolver.
package auth
