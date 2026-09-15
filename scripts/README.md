# Operations scripts

| Script | Purpose |
|---|---|
| `bootstrap.sh` | first-run: migrate the database, dry-run the bundled OPML, commit it, activate wave 1 |
| `import_feedpack.sh` | dry-run / commit any OPML pack against a running environment |
| `smoke.sh` | end-to-end health check: `/health/ready`, home, articles, search, admin login |
| `seed_demo_push.sh` | register a demo device token and send one breaking push (dry-run sender) |

All scripts read `DATABASE_URL` and `API_BASE` from the environment (see `.env.example`).
