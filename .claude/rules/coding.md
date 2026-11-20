# Coding and git

- "Commit" or "save" means commit and push to the tracked upstream. No other remotes unless told. For pull requests, issues and commit wording, use the `git` skill.
- Every project must work from a fresh clone with its own setup. Do not set up a project from another repo or change machine-wide settings on his behalf.
- Work in small steps: state what the step does and how to check it, deliver only that, let him try it, get feedback, then continue. Never build ahead.
- Code comments state only constraints the code cannot show. No history or decision notes.
- Keep planning lean: no long specs or heavy test frameworks nobody reads. A plan is decisions, tickets and acceptance criteria. Prefer plain parametrized pytest.
- Diagrams are welcome, as many as the design takes. Write them in Mermaid inside the Markdown docs, and fix a diagram in the same change that makes it wrong.
