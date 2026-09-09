# Agent Guidance

Follow `AGENTS.md` as the authoritative project instructions.

## Skill routing

When the user's request matches an available skill, invoke it through the skill workflow. When in doubt, invoke the relevant skill.

Key routing rules:

- Product ideas and brainstorming: use `/office-hours`.
- Strategy and scope: use `/plan-ceo-review`.
- Architecture: use `/plan-eng-review`.
- Design systems and design-plan reviews: use `/design-consultation` or `/plan-design-review`.
- Full review pipeline: use `/autoplan`.
- Bugs and errors: use `/investigate`.
- Site behavior QA: use `/qa` or `/qa-only`.
- Code and diff review: use `/review` or `/design-review`.
- Shipping, deployment, or pull requests: use `/ship` or `/land-and-deploy`.
- Session continuity: use `/context-save` or `/context-restore`.
- Backlog-ready specifications and issues: use `/spec`.
