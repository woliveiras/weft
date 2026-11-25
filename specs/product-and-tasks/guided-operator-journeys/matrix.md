# Guided journeys behavior/oracle matrix

Created before implementation on 2026-11-25 from the maintainer-approved UX audit.
Every oracle below is **spec-derived** from TASK-009; tests inspected/extended
after code exists must additionally disclose implementation awareness in evidence.

| ID | Criterion / scenario | Invariant and observable oracle | Verification approach (results and file anchors in evidence.md) |
| --- | --- | --- | --- |
| J01 | AC-001 project/workflow entry | Known context selected once; version/base concrete; no persisted task override | Helper unit; route→editor integration |
| J02 | AC-001 unavailable/late context | No replacement resource; visible correction; explicit edits retained | Helper boundary unit; async UI integration |
| J03 | AC-002 new/existing review | Exact edits saved then reviewed; zero interpretation/start calls | Action helper unit; real API/browser or component boundary integration |
| J04 | AC-002 incomplete/save/review failure | Errors visible; values/revision retained; retry never starts | Failure unit; UI integration |
| J05 | AC-003 authored defaults/legacy fallback | Explicit bindings win; old graphs round-trip; external names are suggestions | Go validation unit; schema/API integration; TS defaults unit |
| J06 | AC-003 invalid bounds/references/channel | Invalid disabled node, mismatched receiver/type or excess bounds rejected | Go unit; API create/update integration |
| J07 | AC-004 readiness/goal/roles | Facts distinguished from checks; unavailable is not ready; admin handoff explicit | View-model unit; editor integration |
| J08 | AC-004 SCM discovery | Pending/error/empty/unavailable distinct; retry restores choices | Discovery helper unit; async field integration |
| J09 | AC-004 repair navigation | Save acknowledged before navigation; failed write stays; return task visible | Context route unit; handoff integration |
| J10 | AC-005 guided creation | Reviewed nodes/channels/modes persisted; no runtime or grant side effect | Builder unit; creation API integration |
| J11 | AC-005 invalid/cancel/advanced | Invalid drafts retained; compatible mode enforced; cancel no write; advanced retained | Builder unit; form integration |
| J12 | AC-006 version adoption | Proposed diff before confirmation; cancel keeps binding/params | Diff model unit; binding component integration |
| J13 | AC-007 unpublished edits | Tabs retain; navigation/version/unload warn; discard explicit; publish failure retains | Guard unit; editor integration |
| J14 | AC-007 association review | Review expected version/changes before write; cancel/failure retain | Review model unit; association integration |
| J15 | AC-008 accessibility/compatibility | Keyboard labels, narrow layouts and existing role/consent gates retained | Browser/responsive inspection; regression suites |
