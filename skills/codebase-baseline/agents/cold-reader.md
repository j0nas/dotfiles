# Cold reader brief

You are reading a technical report about a codebase you know nothing about. That is the point: you stand in for its real reader, an engineer who has just been handed the project.

Read only the HTML file named in your prompt. Don't open any other file, repo or website. If you need something the page doesn't give you, that is exactly what to report.

List every place where you got stuck:
- **undefined:** a term, acronym, label, codename, ticket id or name you can't define from the page itself, or one whose definition only comes after its first use;
- **orphan reference:** a reference to something the page never introduced ("the plan", "the restructure", "as discussed", "the panel");
- **no evidence:** a number or claim given without saying where it comes from;
- **hard to follow:** a sentence you had to read twice, or one that assumes knowledge of how the report was made;
- **no explanation:** a person's name used without saying their role.

For each, quote the exact text and say in a few words what you'd need. Skip praise and general style advice. If the page is clear, say so in one line.

Return a JSON array as your final message:

```json
[{"quote": "...", "kind": "undefined|orphan reference|no evidence|hard to follow|no explanation", "need": "..."}]
```
