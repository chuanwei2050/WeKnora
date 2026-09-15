# KB Maintenance Stop: Interrupt In-Flight Documents

**Date:** 2026-09-15  
**Status:** Approved direction (Approach A) — pending implementation  
**Problem:** Stopping「全部重建 / 重新解析」keeps status `canceling` and holds the maintenance lock until in-flight documents finish. Users wait a long time with other maintenance actions locked.

## Goal

When the user stops a document-pipeline maintenance job (`reparse` / `rechunk`):

1. Immediately mark still-active target documents as failed (interrupted), **without deleting** knowledge rows.
2. Release the maintenance lock right away (`canceled`).
3. Ensure leftover queue workers do not revive interrupted documents back to `processing`.

Non-goals:

- Deleting knowledge documents from the KB.
- Canceling already-running OS/network work mid-byte (best-effort skip on next status check is enough).
- Changing behavior of non-document-pipeline ops (`vector`, `keywords`, etc.) — they already cancel immediately.

## Current Behavior

- `applyKBMaintenanceCancel` sets document-pipeline jobs to `canceling` and keeps the lock.
- Enqueue stops because `IsRunning` is false for non-`running` status.
- Already-enqueued / in-progress document workers continue until `pending`/`processing` (and some summary states) drain to zero.
- UI polls with copy: “在途文档收尾前，其他维护操作仍会锁定”.

## Design

### 1. Interrupt message

Add a dedicated constant in `internal/types/kb_maintenance.go`:

```text
ParseInterruptedByUserCancelMessage = "解析已中断（用户停止重建）"
```

Reuse the same pattern as `ParseInterruptedForRechunkMessage` so leftover workers can recognize a deliberate interrupt.

### 2. Cancel path (`CancelMaintenance`)

For `reparse` / `rechunk` when status is `running`:

1. Call a scoped interrupt: mark `pending`/`processing` documents whose IDs are in `progress.TargetKnowledgeIDs` as `failed` with `ParseInterruptedByUserCancelMessage`.
   - Prefer a new helper (e.g. `InterruptParsesByIDs`) rather than KB-wide `InterruptStuckParses`, so unrelated docs are untouched.
2. Apply cancel so progress becomes **`canceled`** immediately (`FinishedAt` set, lock released).
3. Return the updated progress to the client.

If interrupt partially fails, still prefer releasing the lock after best-effort interrupts, and surface the error in logs / response as appropriate so the UI is not stuck forever.

### 3. Progress store

Change `applyKBMaintenanceCancel` so document-pipeline ops follow the same path as other ops:

- `Status = "canceled"`
- `FinishedAt = now`
- `Message = "canceled_by_user"`

Remove the special-case that kept `canceling` until in-flight drain.

Update unit tests in `kb_maintenance_progress_test.go` accordingly. `canceling` may remain as a readable historical/compat status in the frontend type union, but the happy path after this change should not enter it.

### 4. Worker safety (`ProcessDocument`)

Today only `ParseInterruptedForRechunkMessage` causes a failed document to be skipped; other failed messages can still be overwritten back to `processing`.

Extend the skip check to also treat these as intentional interrupts (no revive):

- `ParseInterruptedByUserCancelMessage` (new)
- `ParseInterruptedForRebuildMessage` (existing; align with rechunk for consistency)

### 5. Frontend copy

Update i18n (`zh-CN` / `en-US`):

- `stopSubmitted`: indicate in-flight docs were interrupted and other maintenance is unlocked.
- Keep or soften `canceling` string for rare leftover Redis state; primary success path shows `canceled`.

No new UI controls; stop button behavior stays one-click.

### 6. Status / progress polling

`GetMaintenanceStatus` already treats `active == 0` as finished. After interrupt, targets that were `pending`/`processing` become `failed`, so `active` drops and no long `canceling` wait is required. With immediate `canceled` from Cancel, polling can stop as soon as the cancel response returns.

## Edge Cases

| Case | Handling |
|------|----------|
| Doc already `completed` | Untouched |
| Doc already `failed` | Untouched |
| Empty `TargetKnowledgeIDs` | Still mark progress `canceled`; optional no-op interrupt |
| Worker mid-write after interrupt | Next load sees failed+cancel message → skip; if a race writes `completed`, accept rare completion of a nearly-done doc |
| Non-pipeline maintenance cancel | Unchanged (already immediate `canceled`) |
| User stops during enqueue staging | `IsRunning` already false; interrupt clears remaining pending/processing targets |

## Testing

- Unit: `applyKBMaintenanceCancel` for reparse/rechunk → `canceled`, lock not busy.
- Unit/service: interrupt-by-IDs only affects listed pending/processing docs.
- Unit: `ProcessDocument` skips leftover jobs when error message is cancel/rebuild interrupt.
- Handler-level or existing rebuild tests: cancel no longer requires waiting for active count if docs are interrupted first.

## Success Criteria

- After Stop on「全部重建」, UI reaches unlocked state without waiting for the previous “28 processing” docs to finish naturally.
- Interrupted docs remain in the KB as `failed` with the cancel message; user can rebuild again later.
- Leftover workers do not flip those docs back to `processing`.
