## Summary

<!-- What does this PR change, and why? One or two sentences. -->

## Component

<!-- Tick everything this PR touches. -->

- [ ] `core/` — Go pricing engine / quoter
- [ ] `executor/` — TypeScript on-chain executor
- [ ] `web/` — Next.js dashboard
- [ ] `docs/` — architecture, design, demo, calibration
- [ ] CI / tooling / repo config

## Changes

<!-- Bullet the concrete changes. Keep it to what a reviewer needs to follow the diff. -->

-
-

## Model / pricing impact

<!--
Delete this section if the PR does not touch fair value, spread, sizing, or
calibration. Otherwise say what moved and what the numbers now are.
-->

- Affects fair value / spread / size: <!-- yes-no + which -->
- Calibration data used: <!-- e.g. docs/calibration.json, N settled markets -->
- Expected effect on quoted spread: <!-- before → after -->

## Testing

<!-- How was this verified? Paste the commands you actually ran. -->

- [ ] `go test ./...` in `core/`
- [ ] `go vet ./...` in `core/`
- [ ] Executor built / ran against Somnia Shannon
- [ ] Web dashboard builds (`npm run build`)
- [ ] Ran end-to-end against a live market

```
<!-- relevant output -->
```

## On-chain / risk

<!-- Delete if this PR cannot place, cancel, or size an order. -->

- [ ] No change to funds-at-risk behaviour
- [ ] Position and exposure limits still enforced
- [ ] Failure mode on RPC error / stale oracle is safe (no unbounded quoting)
- Network tested on: <!-- Shannon testnet / mainnet / none -->

## Config & secrets

- [ ] No new environment variables
- [ ] New variables added to `.env.example` with placeholder values only
- [ ] No keys, addresses, or secrets committed

## Notes for reviewers

<!-- Anything worth flagging: known gaps, follow-ups, why an approach was chosen. -->
