# Specs

This folder contains one spec per topic of concern.

## Template

All specs must follow `specs/_TEMPLATE.md` and include frontmatter.
Completion contracts and verification commands are mandatory to define "done."

## Approval Gate

- Approval is a human decision recorded in spec frontmatter.
- Planning may be automated, but approval is not.
- Approval is recorded when the human reviewer explicitly instructs the agent to mark the spec as approved.
- Changing an approved spec requires explicit human instruction to flip `status` back to `draft` or to update `status: approved`.
