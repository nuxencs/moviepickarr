# ADR 0005: Encrypt persisted integration secrets

Status: accepted (2026-08-04)

## Context

Admin-managed integrations need recoverable credentials, so hashing cannot be
used. Plaintext storage would make every SQLite copy and pre-migration backup a
credential bundle. Requiring an operator-provided key would protect the database
but would make in-app setup depend on environment configuration.

## Decision

Encrypt persisted integration secrets with AES-GCM and a dedicated random
32-byte instance key. Generate the key on first use and store it outside SQLite
with owner-only permissions. Allow an environment-configured secret-file path
for Docker secrets and managed deployments. Environment-managed integration
credentials remain outside SQLite.

The Admin API reports only whether a secret is configured and where its active
value comes from. It never returns the stored value or any fragment of it.

## Consequences

A copied database or migration backup does not reveal integration credentials
by itself. A full restore must include the instance key, unless the deployment
provides it through the configured secret file. Losing or changing that key
makes existing persisted credentials unreadable. The app still starts, marks
affected integrations as `Credential unavailable`, and keeps the ciphertext
until an admin replaces the credential under the current key. Full access to
the running host can still expose both the key and decrypted credentials.
