## Learned User Preferences

- Discuss concrete solutions briefly before making non-trivial edits, and wait for explicit approval unless the user clearly says to fix it now.
- Be concise and action-oriented: avoid restating obvious project context or explaining the user's own system back to them.
- Keep scope extremely tight to the exact request and avoid unrelated cleanup or exploratory changes.

## Learned Workspace Facts

- The visualizer's live telemetry path can involve tens of thousands of values arriving one by one, so hot paths must avoid full rebuilds, unnecessary allocations, and continuous animation.
- In the mesh pipeline, visualizer "communities" correspond to child `Field` ids stamped into the `COMMUNITY` property during routing.
- The intended live visualizer surface is graph-first and field/community-centric, with inspector/detail views as secondary UI.