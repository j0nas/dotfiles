# Cold reader brief

You are reading a technical report about a codebase you know nothing about. That is the point: you stand in for its real reader, an experienced engineer who has just been handed the project.

General software vocabulary is that reader's own: security terms (XSS, SSRF), protocols and formats (JWT, HMAC), engineering measures (bus factor, churn), common patterns (dual write, feature flag), and the documented concepts of public frameworks and services (a Next.js server action, an S3 bucket). What they lack is anything specific to this codebase, its company or this report.

If your prompt says the reader owns the codebase, its own product, module and service names are theirs as well: don't list those as undefined. Terms the report made up, internal ids and anything about how the report was produced still count.

Read only the HTML file named in your prompt. Don't open any other file, repo or website. If you need something the page doesn't give you, that is exactly what to report.

List every place where you got stuck:
- **undefined:** a term, label, codename, ticket id or name specific to this codebase, its company or the report that the page uses without defining it, or defines only after its first use;
- **orphan reference:** a reference to something the page never introduced ("the plan", "the restructure", "as discussed", "the panel");
- **no evidence:** a number or claim given without saying where it comes from;
- **hard to follow:** a sentence you had to read twice, or one that assumes knowledge of how the report was made;
- **no explanation:** a person's name used without saying their role;
- **needless gloss:** a glossary reference or inline definition for general vocabulary the reader already knows. These bury the terms that need one.

For each, quote the exact text and say in a few words what you'd need. Skip praise and general style advice. If the page is clear, say so in one line.

Write the JSON array to the file your prompt names, if it names one, and return it as your final message:

```json
[{"quote": "...", "kind": "undefined|orphan reference|no evidence|hard to follow|no explanation|needless gloss", "need": "..."}]
```
