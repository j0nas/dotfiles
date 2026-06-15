When a page won't load through the built-in fetch tool (WebFetch) — it errors, returns an empty/garbled or suspiciously short body, a bot/CAPTCHA wall, an "enable JavaScript" stub, a login redirect, or content that's obviously JS-rendered and missing the data you came for — fall back to the `/agent-browser` skill. It drives a real browser that runs JavaScript, holds cookies/sessions, and can screenshot or extract the rendered DOM. Don't retry-loop WebFetch or tell the user the content is inaccessible until you've tried agent-browser.

## Why

WebFetch pulls static HTML and presents a non-browser client to the server. Modern sites increasingly render content client-side, gate it behind bot protection (Cloudflare et al.), or require an authenticated session — all of which return nothing useful to a plain fetch even though the page is perfectly readable in a real browser. agent-browser closes that gap instead of dead-ending the task.

## How to apply

- Trigger on: an HTTP error (403/429/503), an empty or implausibly short body, a "turn on JavaScript" placeholder, a bot/CAPTCHA or login wall, or a body that's clearly missing the content you need.
- Flow: try WebFetch first (it's cheaper); on any of the above, invoke `/agent-browser` to navigate, wait for render, then screenshot or extract the DOM/text.
- Reach for it directly — skipping WebFetch — when the task inherently needs a browser: filling forms, clicking, pagination, or anything behind a login or session.
