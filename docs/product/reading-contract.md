# Product contract (v1)

## What the reader gets

| Surface | Content |
|---|---|
| Home | breaking strip, top stories, latest Afghanistan, followed province, economy, jobs, world |
| Afghanistan / World | infinite lists with latest ⇄ top ordering |
| Provinces | all 34 provinces, followable, with recent story counts |
| Sources | the publisher registry with trust weight and transparency labels |
| Search | server-side full-text with offline repeats of previous queries |
| Saved | on-device bookmarks, readable without network |
| Notifications | in-app inbox fed by pushes; readable offline |
| More | language, theme, text size, push topics, cache budget, about |

## Non-negotiables

1. **No mandatory login.** v1 has no accounts. Personalisation is on-device.
2. **Attribution is always visible.** Source name, type and transparency label appear on every
   card and every detail page; the original URL is one tap away (Chrome Custom Tabs).
3. **No republishing.** The app shows the feed-provided summary/HTML and links out. It never
   presents itself as the publisher of a full article.
4. **Four states everywhere.** Loading, Content, Empty, Error. A screen that can only show
   "loading" and "content" is unfinished.
5. **Offline is a first-class state**, not an error: cached Home renders, saved articles open,
   the inbox works, and a banner explains the situation.
6. **Rates are never guessed.** Currency/fuel rates appear only from a dedicated provider —
   never extracted from headlines.
7. **Persian-first typography**: minimum 13.5sp body with 26sp line height, 48dp touch targets,
   RTL by default, Pashto and English available.

## Editorial workflow

1. Feeds are imported from the OPML pack (dry-run first, always).
2. The worker ingests, classifies and clusters; breaking evaluation runs on every insert.
3. Editors moderate in the console: hide junk, re-classify, flag/unflag breaking, compose pushes.
4. Rollout waves expand the catalog only after the current wave's health is stable.
5. Everything is audited; nothing destructive is possible from the UI.

## Deliberately out of scope for v1

* Accounts, comments, user-generated content.
* Full article archiving/scraping (both a legal and an ethical line).
* Any monetisation surface inside the reader.
* Video hosting or transcoding.
