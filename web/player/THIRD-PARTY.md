# Player dependencies and application marks

The player reuses React, TypeScript, Vite, and Playwright versions from the
administrator toolchain. The lockfile is the dependency inventory for this
independent package. React and hls.js are MIT licensed. Noto Sans SC, Noto Serif
SC, League Gothic, and IBM Plex Mono are bundled through Fontsource; their font
licenses are SIL Open Font License. Package license texts remain in the
corresponding installed packages.

Application icons identify the corresponding external application. They do not
imply affiliation, an installed application, or a working protocol handler.
The assets were obtained from these first-party distribution pages on
October 4-5, 2026:

| Asset | Source |
| --- | --- |
| IINA | [Official website icon](https://iina.io/img/icon.png) |
| VLC | [VideoLAN website icon](https://images.videolan.org/images/icons-VLC/vlc.mini.svg) |
| nPlayer | [Official website touch icon](https://nplayer.com/assets/img/apple-icon-180x180.png) |
| MX Player | [Developer application listing on Google Play](https://play.google.com/store/apps/details?id=com.mxtech.videoplayer.ad) |
| Infuse | [Firecore application listing on the App Store](https://apps.apple.com/app/infuse/id1136220934), artwork supplied by Apple's lookup endpoint |
| PotPlayer | Native PNG icon resource from `PotPlayerMini64.exe` in the [official Kakao installer](https://t1.daumcdn.net/potplayer/PotPlayer/Version/Latest/PotPlayerSetup64.exe), product version `1.7.23118.0`; extracted without executing or installing it |
| Stellar Player | PNG favicon embedded in the [official website](https://www.stellarplayer.com/) |
| mpv | [Official source repository icon](https://github.com/mpv-player/mpv/blob/v0.41.0/etc/mpv-icon-8bit-128x128.png) |
| Dandanplay | [Official documentation icon](https://doc.dandanplay.com/images/logo.png) |

The inspected PotPlayer installer SHA-256 is
`4b9833adf53b78c2149fe13dd67f2504bf39ee30b45c03f09e5f2c25e43f9cfe`.
Its Kakao Corp. Authenticode signature and timestamp were verified in `test-env`.
The copied icon SHA-256 is
`a8894c2f80e13a9b120ed4de53fdedca29c1976f125dd3d74abe13cf3f636ba1`.
The installer itself is not bundled with Goby.

The names, marks, and artwork remain the property of their respective owners.
Goby's application license does not relicense these marks. The original
handoff's invented letter tiles are not used for external applications.

## External application URL contracts

The original HTTP(S) media URL remains the payload. External playback uses the
existing direct-file negotiation; it does not introduce transcoding or a local
launcher service. A launch link means that the client has a documented or
first-party-verified protocol contract. It cannot establish that the application
is installed, associated with the protocol, or permitted by the browser.

| Application and platform | Contract and evidence |
| --- | --- |
| PotPlayer, Windows | `potplayer:` followed by the complete media URL. Static inspection of the signed Kakao installer above confirms that `PotPlayer64.dll` recognizes and removes either `potplayer:` or `potplayer://` and registers the `potplayer` URL protocol. Goby uses the opaque form without `//` to preserve the nested HTTP(S) scheme during browser URL parsing. The URL is not encoded a second time because the handler passes the remainder directly to the player. |
| IINA, macOS | `iina://weblink?url=` with an encoded media URL; [official application URL handler](https://github.com/iina/iina/blob/develop/iina/AppDelegate.swift). |
| Infuse, iOS and macOS | `infuse://x-callback-url/play?url=` with an encoded media URL; [Firecore URL scheme documentation](https://support.firecore.com/hc/en-us/articles/215090997-Callback-API). |
| VLC, iOS | `vlc-x-callback://x-callback-url/stream?url=`; [VideoLAN iOS documentation](https://wiki.videolan.org/Documentation:IOS/). |
| VLC and MX Player, Android | Android `ACTION_VIEW` intents scoped to their application packages. Browser handling follows [Chrome's Android intent contract](https://developer.chrome.com/docs/android/intents). |
| mpv, Windows, macOS and Linux | `mpv://` with a percent-encoded media URL, introduced in mpv `0.41.0`; [official protocol documentation](https://github.com/mpv-player/mpv/blob/v0.41.0/DOCS/man/mpv.rst). A registered system handler is required. The [protocol demuxer](https://github.com/mpv-player/mpv/blob/v0.41.0/demux/demux_mpv.c) decodes once. The [macOS application entry point](https://github.com/mpv-player/mpv/blob/v0.41.0/osdep/mac/app_hub.swift) also decodes once, so its link has an additional encoding layer to preserve escaped path and query values. |
| Dandanplay, Windows | `ddplay:` followed by the encoded complete media URL, supported by PC version `12.2+`; [official client protocol documentation](https://doc.dandanplay.com/open/client-protocol.html). No filename or extra command is inferred from the display title. |
| Dandanplay, Android | Android `ACTION_VIEW` intent scoped to `com.xyoye.dandanplay`. The [official application manifest](https://github.com/xyoye/DanDanPlayForAndroid/blob/master/player_component/src/main/AndroidManifest.xml) exposes a browsable HTTP(S) media receiver, and [the receiver implementation](https://github.com/xyoye/DanDanPlayForAndroid/blob/master/player_component/src/main/java/com/xyoye/player_component/ui/activities/player_intent/PlayerIntentActivity.kt) consumes the intent URL. |

The handoff's full player list is retained with its platform filters. Desktop
VLC, nPlayer, Stellar Player, and macOS Dandanplay currently offer the original
stream URL for manual opening. A first-party browser-launch contract was not
confirmed for those combinations during this task. For desktop VLC, the
[macOS bundle](https://github.com/videolan/vlc/blob/master/share/Info.plist.in)
and [Windows installer](https://github.com/videolan/vlc/blob/master/extras/package/win32/NSIS/vlc.win32.nsi.in)
do not establish a dedicated `vlc://` media-launch wrapper. Stellar documents
[opening a network URL manually](https://www.stellarplayer.com/FAQ). These are
external-client integration limits, not missing Goby backend capabilities.

The standalone package does not change the repository's existing public
distribution or project-license decisions. See the root
[third-party notices](../../THIRD_PARTY_NOTICES.md).
