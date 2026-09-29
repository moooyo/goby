# Docker-only delivery policy

Decision date: **September 30, 2026**. The user selected **Docker as Goby's only
supported delivery form**. This policy supersedes earlier plans that list native
packages or other container runtimes as future delivery work.

## Supported delivery

Goby is delivered as a Docker image for Docker Engine, with Docker Compose
configuration and an operator guide. The current distribution is an importable
image archive, accompanied by its immutable image identity, checksums, build and
acceptance records, configuration examples and required deployment companions.

| Current profile | Delivery and acceptance |
| --- | --- |
| Linux amd64 software media | [Software Docker/Compose guide](../../deploy/oci/README.md) and [acceptance record](../development/oci-delivery-20260929.md) |
| Linux amd64 selected AMD media | [AMD Docker/Compose guide](../../deploy/oci/README.amd.md) and [acceptance record](../development/oci-amd-delivery-20260929.md); includes its render-device and seccomp configuration |

Software and AMD are profiles of the same Docker delivery form. Their recorded
host, architecture, engine, device/driver and media limits still apply. Adding
another architecture or GPU requires a separately selected Docker image profile;
Docker-only delivery does not establish arm64 or universal hardware support.

A container registry may later distribute the same Docker images if publication
is selected. It is a distribution channel, not another delivery form. No registry
publication or production deployment is implied by this policy. The existing
`OCI` names in directories, receipts and documentation describe the image work
and artifact format; they do not promise support for other container runtimes.

## Outside the supported delivery scope

The following are not supported distribution or installation options and are not
deferred release obligations:

- Standalone host binaries or native systemd installation archives.
- Native OS packages or installers, including DEB/RPM and Windows installers.
- Non-Docker container runtimes or additional orchestration-specific packages.

Existing native installation documents, scripts, builds and acceptance records
remain available as development tools or historical evidence. They do not define
a current supported installation path. This decision does not delete source code
or tools, invalidate their recorded tests, or require rebuilding accepted images.

Source builds and native verification fixtures may still be used for development
under the task's environment rules. Binaries, the recovery CLI, FFmpeg and native
helpers inside the Docker image remain image components, not separate host-level
deliveries. Keep operational recovery instructions applicable to the Docker
installation and its external database and persistent state.

## Dependencies and future work

This is a policy for delivering **Goby**. External PostgreSQL, media storage and
reverse proxies retain the configuration and operating choices documented in the
Docker guides; this decision does not require those dependencies to run in Docker.
Project licensing and public-distribution decisions also retain their own scope.

Current plans, support tables and operator entry points must route installation,
upgrade and rollback through the Docker guides. Historical native-package gaps
must not be reintroduced into the current release checklist. Any later change to
this delivery policy requires an explicit product decision.
