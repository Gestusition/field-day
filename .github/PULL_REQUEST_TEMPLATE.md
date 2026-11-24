## What and why

<!-- What does this PR change, and why? Link the issue: "Closes #123". -->

## Area

- [ ] Backend (`apps/api`)
- [ ] API contract (`api/openapi.yaml`)
- [ ] Database migration
- [ ] Web
- [ ] Mobile
- [ ] Firmware
- [ ] CI / infra / docs

## Contract and data changes

<!-- Delete what does not apply. -->

- [ ] **No** change to `api/openapi.yaml`
- [ ] Contract changed **compatibly** (new optional field / new endpoint)
- [ ] Contract changed **incompatibly** — affected teams (web / mobile / firmware) are tagged below
- [ ] New migration; it only adds things or is safe on a live database (Squawk passes)
- [ ] Generated code regenerated (`make gen`) and committed

Teams to notify:

## How it was tested

<!-- Commands you ran, manual steps, screenshots for UI. -->

- [ ] `make test` passes locally
- [ ] Tested manually (describe):

## Checklist

- [ ] Small, focused PR with a Conventional Commit title (`feat(api): ...`)
- [ ] No secrets, keys, personal data or `.env` files committed
- [ ] Docs updated (README, `docs/`, AGENTS.md) if behaviour or setup changed
