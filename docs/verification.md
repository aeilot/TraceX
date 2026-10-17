# Verification

The initial implementation was verified on macOS with Go 1.26.4 and Node.js 22.21.1.

- `make check`: Go tests, Node model/validation tests, JavaScript syntax, Go race tests, vet, and binary build passed.
- Browser checks: DAG/timeline/split switching, agent search, type and task filters, details opening and Escape dismissal, timeline zoom/reset, cycle rejection on import, valid custom-profile import, export download, source reset, and a 390-pixel viewport without page overflow passed. No browser console errors were observed.
- An existing Hermes ARIES run loaded two task occurrences, 193 spans, 28 explicit parent edges, and 345 resource samples. Task filtering was verified in the UI. An absent agent identity displayed as unavailable, while later measured zero CPU samples remained zero.
- An existing OpenClaw ARIES run loaded one task occurrence, 81 bridge spans and 317 resource samples. No parent edges were fabricated. The unsupported native trajectory adapter was reported explicitly.

The real run artifacts used for these checks are private and are not included in this repository. The screenshot in the README contains only the built-in synthetic demo.

To repeat the automated checks:

```sh
make check
```

Manual browser check: run `make run`, switch all three layouts, select nodes/bars, search an agent, change the span-type filter, zoom/reset, then import an invalid cyclic profile and confirm it leaves the active dataset unchanged. Start with `--run PATH` to verify your own backend evidence. The app is a fixed offline snapshot; refresh of a running experiment is not implemented.
