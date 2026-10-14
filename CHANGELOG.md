# Changelog

## Unreleased (auto-update)

- Online updates: Kumo checks at launch and once a day (Settings › Updates: on/off, Stable or Beta channel, Check now) and offers a new version with its notes: Update now, Later (also Escape), or Skip this version.
- Updates are verified before anything changes: the manifest (`latest.json`) is signed with Ed25519, and every file has its own SHA-256 and signature, checked against the public key built into Kumo. The release workflow signs with the `KUMO_UPDATE_KEY` secret and refuses a key that does not match.
- The download runs in the background with progress and resumes after an interruption. Quitting restores the system proxy as always; then a helper swaps in the new `Kumo.app` (quarantine removed from our own verified copy), portable or `tar.gz` folder, or runs the Windows installer silently, and starts the new version. If it does not start, the previous version is put back and Kumo says so. A `.deb` install is downloaded, verified and opened in the package manager.

## Unreleased (stats-hotkeys)

- Statistics page (sidebar, also in simple mode): today, this month and the range's traffic, a daily (or monthly) chart, top apps, nodes and proxy groups; 7/30/90 days or 12 months; Clear. Counted from the core's totals and live connections, aggregates only, in one small gzip JSON file (`traffic-stats.json.gz`, atomic writes), 90 days daily and 24 months monthly, top 20 per day and 100 per month with the rest under Other. A simulated heavy year (500 apps, 300 nodes, 40 groups every day) is about 14 KB.
- Node monitor: background latency tests of every node (default every minute, 3 at a time, spread over the interval, the group's test URL), kept in memory for this launch: uptime, average, p95, jitter, last up, outages. Cards show uptime (and a small latency line in advanced mode); the activity button opens the node's sheet with the timeline and outages.
- Node tests in the sheet: download speed (Cloudflare, up to 8 s or 50 MB, cancelable, data use explained first) and UDP (a DNS query over SOCKS5 UDP), through a hidden selector and a loopback-only listener, so the groups' choices are not touched.
- Global shortcuts (Settings): system proxy, Enhanced Mode, next mode, show/hide; none by default; recorded by pressing them, with conflict and registration errors shown.

## 0.4.4

- Enhanced Mode re-authorization: Escape or closing the dialog now means Later (the setting stays on, the core runs without it now, Kumo asks again next start); only “Turn off Enhanced Mode” turns it off.
- macOS helper: the root core's controller is a unix socket only, reached through a root-owned filter that requires the secret and re-validates every configuration loaded (no listeners, no path loads, only Kumo's PATCH keys, no upgrade/debug endpoints).
- macOS helper: the client must match Kumo's code-signature cdhash (when the build is signed), not just its process name.
- System proxy restore on macOS: an empty original server is cleared, and turned off even if networksetup refuses it; checked with a fake and the real networksetup in CI.

- The round button at the top left now refreshes (groups and nodes with their latency, configuration, rules) without restarting the core; ⌘R does the same.
- Restart core moved to the Core page and asks for confirmation first.
- First launch follows the system language (macOS AppleLanguages, e.g. zh-Hant / zh-Hans / en; LANG elsewhere): any Chinese → 繁體中文, otherwise English. The choice is saved, so the interface and the Settings selector always agree; values from older versions ("", "system") are resolved the same way.
- Live speed beside the tray icon, upload over download, about once a second (macOS: two monospaced lines drawn into the template icon; Linux: indicator label; Windows: tooltip). On by default; Settings → Show speed in menu bar.

Security and reliability (fixes of the 0.4.3 review):

- Enhanced Mode on macOS no longer installs a setuid-root core. A per-user launchd helper runs the core as root in a root-owned home, with the configuration Kumo hands over; it refuses configurations that open listeners or hide keys, forces the controller onto 127.0.0.1, reads the user's files as the user, and stops the core when Kumo goes. Kumo runs a small client in the core's place.
- Enhanced Mode privileges only the core Kumo ships: its SHA-256 is pinned at build time (`corepin`, checked against `core.lock.json` in tests and against the packaged app in CI), and verified by root on a root-owned copy before rights are granted. A core found on PATH is never privileged.
- Linux: the capability core moved to `/usr/local/lib/kumo/<uid>/`, runnable only by its user (private group, else an ACL); the install script reports missing tools and mismatched cores precisely (no more "denied" for "not found"). Kumo explains what will be installed before the system's password dialog.
- Kumo 0.4.3's world-runnable privileged copy is offered for removal at launch; root-owned files it left in the core folder are removed.
- Turning Enhanced Mode off restarts the core without privilege. When Enhanced Mode is set but no longer authorized (after an update), Kumo says so and offers to set it up again or turn it off.
- Subscriptions can no longer open listeners: `listeners`, `tunnels`, `redir-port`, `tproxy-port`, server inbounds and a non-loopback `dns.listen` are dropped, and the `tun` block is Kumo's own (only the stack is kept).
- Quitting restores the system proxy first (own 8 s budget, every macOS network service in parallel), then stops the core, so apps never point at a dead port.
- The system proxy now saves and turns off PAC and auto-discovery (macOS and Windows) and restores them; restoring clears Kumo's address on macOS; with no backup, Kumo only turns off proxies that point at itself.
- Core (re)starts and subscription switches are serialized, newest wins. A controller port taken between check and start is retried on another port. Start and pid-file errors are reported.
- Windows: the core runs in a job object (ends with Kumo even after a crash) and is asked to take TUN down before it is killed. Unix: the watchdog wakes on a pipe from Kumo and checks the core's identity before signalling it, so a reused pid is never hit.
- Subscriptions: share-link lists (plain or base64) are recognized and explained, with a “&flag=clash” hint; plain-http links get a warning. Errors are typed rather than matched by English text.
- External controller: optional HTTPS/WSS, a warning for remote hosts without it, and the secret goes in a header (never in WebSocket URLs). `KUMO_SECRET` is used but no longer saved.
- Corrupt settings or subscription lists are kept aside and reported instead of being silently overwritten.
- Config reloads send the configuration text instead of a path.

## 0.4.3

- Dock menu on macOS (right-click the Dock icon), and the same menu in the
  tray: system proxy, Enhanced Mode, Rule / Global / Direct, a submenu per
  proxy group to switch its node or use the fastest, Open and Quit (the Dock
  has its own Quit). The menus follow Kumo's state.
- Enhanced Mode (增強模式, mihomo's TUN): a compact switch on the home
  screen's system-proxy tile, in the menus and on the Core page. The first
  time it asks once for an administrator: macOS installs a root-owned setuid
  copy of the core in /Library/Application Support/Kumo; Linux copies it to
  /usr/local/lib/kumo and grants cap_net_admin with pkexec and setcap;
  Windows needs Kumo run as administrator. Refusing leaves it off with a
  clear message; a Kumo update asks again.
- A watchdog ends the core if Kumo dies without stopping it, as the kernel's
  parent-death signal does not reach a privileged core.

## 0.4.2

- The interface follows the v0.3.1 design exactly again, measured from
  v0.3.1 itself: window tint and gradient (with the eight sidebar colors),
  200 pt sidebar with 38 pt rows, the sidebar and restart-core buttons beside
  the window controls, home tiles and their order, node group cards and node
  cards, subscription rows, tables of rules and connections, the log list,
  and the settings and core pages. Written anew in Kumo's code.
- Logs: level, pause and resume again.
- Search fields show "搜尋"; the configuration view shows emoji as characters.

## 0.4.1

- The v0.3.1 look is back, on the 0.4 code: a softly tinted gradient window
  (light blue by day, indigo to navy in the dark theme), a sidebar floating on
  it with quick "hide sidebar" and "refresh" buttons and Settings at the
  bottom, list pages on a rounded sheet, and rounded cards with a soft shadow.
- Home: 130 pt tiles, edge-to-edge traffic and memory charts (violet upload,
  cyan download), the address, node, mode and core version on one line, and
  a connections tile with upload, download and rule totals.
- Nodes: two-line node cards with the latency badge, larger group titles and
  buttons.
- Search fields show a translated hint ("篩選規則"…) instead of "Search".
- The configuration view shows emoji (flags in node names) as characters, not
  \U escapes.

## 0.4.0

- Rewritten from scratch on [MyGo](https://github.com/egoist/mygo): a new
  interface (MyGo's native sidebar and tables, a new card design, charts,
  accent colors), new app state and tray code, and a new core fetcher and CI
  workflow.
- Kumo is now MIT licensed; the bundled mihomo core stays GPL-3.0 as a
  separate program.
- Same features as 0.3.1: home dashboard with a compact connect switch,
  nodes with latency and "use fastest", subscriptions, rules, connections
  (now a resizable table with per-connection close), logs, settings, and
  advanced mode (core settings, running configuration), tray, ⌘Q, ⌘R, ⌘,.
- Six accent colors; Lucide icons fetched from lucide-static.

## 0.3.1

- Home is the v0.2.0 dashboard again (tiles, sparklines, memory,
  connections, subscription) with a compact system-proxy switch, the mode
  and the node with its latency; shorter copy, no oversized button.
- No more full-page onboarding: a slim hint links to 訂閱 until a
  subscription is added.
- Rules, connections and logs are back in the sidebar; 進階模式 adds only
  the core settings and the configuration view. Settings sit at the bottom
  of the sidebar again.
- Sharper README screenshots (2x) with live traffic and real latencies.

## 0.3.0

- A home screen for beginners: paste a subscription link on first run and
  Kumo imports and uses it; then one big button connects (system proxy),
  with the node, the mode and plain explanations of 規則 / 全域 / 直連.
- Traditional Chinese by default, plain labels and help text everywhere;
  rules, connections, logs and core settings moved under 進階 in the
  sidebar.
- Nodes: group cards, the node in use highlighted, colored latency with
  timeouts, test all, sort by latency, switch to the fastest node.
- Clear messages for failed subscriptions, a core that does not start and a
  system proxy that needs administrator rights or GNOME; loading states and
  confirmation before deleting a subscription.
- ⌘Q, the app menu's Quit and the tray's Quit now always quit Kumo (they
  were cancelled by the window hiding itself to the menu bar), stopping the
  core and turning the system proxy off. Closing the window still only
  hides it while a tray / menu bar icon exists.
- Simple by default; **進階模式** in Settings adds rules, connections, logs,
  the core (status, version, restart moved off the home screen) and a
  read-only, copyable view of the running configuration. ⌘R refreshes,
  ⌘, opens the settings. The home screen shows the core only when it fails
  (「核心未執行」 with 重新啟動).
- System proxy: your previous settings are saved and restored on
  disconnect and quit, and repaired at launch after a crash.
- Quitting stops the core and restores the proxy in parallel, at most 2.5 s.
- Robustness: core start/stop serialized, an orphaned core from a crash is
  stopped at launch (and on Linux dies with Kumo), thread-safe profiles,
  subscription downloads with a timeout, a 32 MB limit, no HTTPS→HTTP
  redirects and atomic writes; subscriptions can no longer open the
  controller, the LAN, TUN or a web UI behind your back; WebSocket
  reconnects back off up to 30 s.
- CI: read-only token for builds, write only for the release; SHA256SUMS
  with every release; tests run with the race detector; a quit test.
- Tray: status, system proxy, mode, quit. The system proxy now stays off at
  launch unless "啟動時自動開啟系統代理" is on.

## 0.2.0

- Works out of the box: the mihomo core (v1.19.32) ships inside every
  package, starts with Kumo on its own port and secret, and stops on quit.
  Restart it from the Overview, Settings or the tray; an external controller
  can still be used instead.
- Profiles: add subscriptions (with usage, expiry and auto update) or import
  local files, and switch between them without restarting.
- System proxy switch (macOS, Windows, GNOME on Linux) and TUN mode switch,
  in the Overview, Settings and the tray.
- The Windows download is now an installer plus a portable zip; Linux ships
  `.deb` and `.tar.gz` (both with the core).

## 0.1.1

- New Kumo cloud icon for the app, installers and tray.

## 0.1.0

- First release, a Clash / mihomo dashboard: overview, proxies, rules,
  connections, logs and settings over the external-controller API.
