# Third-party notices

Kumo's own code is MIT licensed (see `LICENSE`). It uses or ships the
following third-party components.

## Shipped in every package

| Component | License | Notes |
|---|---|---|
| [mihomo](https://github.com/MetaCubeX/mihomo) v1.19.32 | GPL-3.0 | Separate executable in `core/`, run as its own process and not linked. Its license is `core/LICENSE-mihomo.txt`; its source is at https://github.com/MetaCubeX/mihomo/tree/v1.19.32. Kumo ships the unmodified official release binaries, pinned by SHA-256 in `scripts/fetch-core/core.lock.json`. |
| [Lucide](https://lucide.dev) icons (lucide-static 0.469.0) | ISC | SVG files in `icons/`. |

## Compiled into Kumo

| Module | License |
|---|---|
| [github.com/egoist/mygo](https://github.com/egoist/mygo) | MIT |
| [github.com/coder/websocket](https://github.com/coder/websocket) | ISC |
| [gopkg.in/yaml.v3](https://github.com/go-yaml/yaml) | MIT and Apache-2.0 |
| [golang.org/x/sys](https://pkg.go.dev/golang.org/x/sys) | BSD-3-Clause |
| [github.com/ebitengine/purego](https://github.com/ebitengine/purego) | Apache-2.0 |
| [github.com/go-text/typesetting](https://github.com/go-text/typesetting) | BSD-3-Clause / Unlicense |
| [golang.org/x/image](https://pkg.go.dev/golang.org/x/image), [golang.org/x/text](https://pkg.go.dev/golang.org/x/text) | BSD-3-Clause |

### Lucide (ISC)

Copyright (c) for portions of Lucide are held by Cole Bemis 2013-2022 as part
of Feather (MIT). All other copyright (c) for Lucide are held by Lucide
Contributors 2022.

Permission to use, copy, modify, and/or distribute this software for any
purpose with or without fee is hereby granted, provided that the above
copyright notice and this permission notice appear in all copies.

THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES WITH
REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF MERCHANTABILITY
AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR ANY SPECIAL, DIRECT,
INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES WHATSOEVER RESULTING FROM
LOSS OF USE, DATA OR PROFITS, WHETHER IN AN ACTION OF CONTRACT, NEGLIGENCE OR
OTHER TORTIOUS ACTION, ARISING OUT OF OR IN CONNECTION WITH THE USE OR
PERFORMANCE OF THIS SOFTWARE.
