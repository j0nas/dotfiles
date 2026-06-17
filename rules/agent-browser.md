When WebFetch can't load a page — HTTP error (403/429/503), empty/garbled/suspiciously short body, bot/CAPTCHA or login wall, "enable JavaScript" stub, or content clearly JS-rendered and missing the data you need — fall back to the `/agent-browser` skill. It drives a real browser: runs JS, holds cookies/sessions, screenshots or extracts the rendered DOM. Don't retry-loop WebFetch or call content inaccessible until you've tried it.

- Flow: try WebFetch first (cheaper); on any of the above, use `/agent-browser` to navigate, wait for render, then screenshot or extract.
- Skip WebFetch entirely when the task inherently needs a browser: forms, clicking, pagination, or anything behind a login/session.
