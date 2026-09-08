# Goby

Goby is a planned open-source media server for Linux, implemented in Go, with an Emby-compatible API and a React + Material UI administrator dashboard.

The backend will serve existing clients, including media streaming and playback state. The dashboard will manage users, libraries, metadata, sessions, transcoding, and system settings. It will not include a web player or a consumer media browsing application.

Implementation is underway using PostgreSQL, Go 1.27.1, and FFmpeg 9.0.1. It now includes authentication, a persistent media catalog, safe file probing, bounded scans, local NFO metadata, persistent genre/tag/studio/person identities, indexed artwork with image transformations and caching, library-level access control, initial Emby browsing, and React/MUI user/library/task administration. Playback work follows, including hardware decoding and encoding. See [build/run instructions](docs/development/running.md), [local metadata](docs/development/local-metadata.md), [local artwork](docs/development/local-artwork.md), and [implementation progress](docs/development/progress.md).

Start with the [documentation guide](docs/README.md), the [implementation scope](docs/api/implementation-scope.md), and the [Linux architecture](docs/architecture/linux-go-react.md).

The primary research baseline is the official Emby SDK **4.9.5.0 Release**, pinned to commit `bdd0dd7c0801f6e069dff2795d80cddae6f91791`. Its API export contains **535 HTTP operations across 422 paths**, grouped into **70 services**, and **333 schema definitions**. These are upstream inventory counts, not Goby implementation counts.

The older static Swagger browser is separately recorded because it exposes a different API surface. Neither document proves that all released clients will work without behavioral testing.
