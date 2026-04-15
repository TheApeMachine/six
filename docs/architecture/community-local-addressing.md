# Community-Local ValueID Addressing

Goal: enable fire-and-forget ValueID-targeted calls without any global registry, using the already-working chain:
- link
- affinity
- route into community field
- community residency

This document defines the next-stage design after link -> affinity -> route became real.

## Non-negotiable rules

1. No global ValueID registry.
2. No host-side lookup table mapping ValueID -> Value pointer.
3. A ValueID-targeted call may only be resolved inside communities that already hold resident Values.
4. Gossip is the propagation mechanism between communities / nodes.
5. Finalizer may publish a call or reschedule a resident, but may not emulate addressing logic that belongs in the field / gossip layer.

## Current substrate facts

These are already true in the codebase:

- `Router.Route` assigns settled Values into community fields based on affinity distance.
- `geometry.Field.Values` already holds resident Values for a community.
- `gossip.Conn` can transport full Value wire frames without a registry.
- `PriorityRoute` and `AffinityFilter` already give affinity-local propagation.
- Link and affinity now run before routing, so communities are meaningful local populations.

Therefore, the missing piece is not storage. The missing piece is a protocol for:
- representing a call to ValueID X
- propagating that call through gossip to the right locality
- having the resident Value with ID X reschedule itself onto the pool

## Desired behavior

A program may eventually write `next <ValueID>`.

When that happens:
1. The completed Value does not perform a host-side lookup.
2. Instead, it emits or publishes a call Value/frame carrying:
   - target ValueID
   - caller identity if useful
   - optional payload/context for the callee
3. Gossip propagates that call according to affinity / locality.
4. Communities inspect incoming calls against their resident `Values`.
5. If a resident Value with the target ID exists in that community, that resident is scheduled onto the pool.
6. If not, the call continues propagating or expires.

This keeps addressing local and emergent.

## Representation choice

Do not overload direct scheduler semantics with global meaning.

Recommended design:
- represent a call as a dedicated ephemeral Value
- target ValueID is encoded in a canonical region, likely inside `asset` or `properties`
- the call Value carries its own affinity so it naturally routes toward the relevant community

Why a dedicated call Value is preferable:
- it fits the existing substrate story: everything is a Value
- it travels through gossip unchanged
- communities can inspect it using the same resident-state model
- no out-of-band control channel is required

## Resolution boundary

Resolution happens at the community boundary, not globally.

That means the matching algorithm should be:
- community receives an incoming call Value
- community scans only `field.Values`
- if any resident Value has `value.ID() == targetID`, the community schedules that resident
- otherwise the call is not resolved there

This is the crucial locality rule.

## Affinity and propagation

A call Value still needs an affinity.

Recommended rule:
- derive call affinity from the target-local context already known by the caller
- or from the callee's last known affinity if the caller has it in-band
- otherwise derive from the caller's own local context and let gossip converge by affinity locality

Important:
- ValueID is the exact address inside a community
- affinity is the routing bias between communities

So:
- affinity gets the call near the right locality
- ValueID resolves the exact resident inside that locality

## Orchestrator responsibilities

The orchestrator should not become a global address resolver.

Allowed responsibilities:
- when a completed Value requests `next <otherID>`, publish a call Value into the normal pipeline / gossip path
- when a call has already been locally resolved by a community, schedule the matched resident Value onto the pool

Not allowed:
- searching all communities globally for a target ValueID
- storing a registry of known ValueIDs

## Community responsibilities

A community field should eventually gain one addressing-oriented operation:
- inspect incoming call Values
- match target ID against resident `Values`
- on match, return or emit the matched resident for scheduling

This is local state inspection, not a registry.

A likely API shape later could be something conceptually like:
- `ResolveResidentByID(target uint64) *primitive.Value`

But implementation must stay local to the community object.

## Gossip responsibilities

Gossip should carry call Values exactly like any other Value frame.

Potential future enhancements:
- TTL / depth for call Values so unresolved calls die out
- affinity steering for calls
- suppression of repeated already-seen unresolved calls

But none of that should be required for the first implementation.

## Minimal first implementation plan

1. Define a canonical call Value representation.
2. Add a community-local resident match path over `field.Values`.
3. On `next <otherID>`, publish a call Value instead of attempting lookup.
4. When a community resolves a call locally, schedule the resident onto the pool.
5. Add a focused test:
   - two resident Values in communities
   - publish a call targeting one resident ID
   - verify only the matching resident is scheduled

## What not to do

- No `map[uint64]*Value` registry.
- No `Router.FindByID` global search.
- No orchestrator-wide scan over all communities.
- No direct host lookup during finalization.

## Verification target

A correct implementation will satisfy this statement:

"A ValueID-targeted call is resolved only by communities that already hold the resident Value, and the only cross-community propagation mechanism is gossip over Value frames."
