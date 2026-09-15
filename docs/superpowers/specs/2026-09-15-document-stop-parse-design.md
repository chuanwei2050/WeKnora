# Document Stop Parse Design

**Date:** 2026-09-15  
**Status:** Approved (Approach A + batch)  
**OpenSpec:** `openspec/changes/document-stop-parse`

## Problem

Parsing documents still surface「重建」, with no single or batch stop that clears `image:multimodal` asynq tasks and `multimodal:pending:*` Redis keys.

## Goal

1. Show「停止」instead of「重建」while parsing (list + card + batch bar).
2. Confirm → mark target docs `failed` (keep rows) → purge pipeline leftovers.
3. **Single and batch** both MUST clear `image:multimodal` and `multimodal:pending:*` for each interrupted ID.
4. Workers must not revive interrupted docs.

## Approach

- `POST /knowledge/{id}/stop-parse`
- Batch stop-parse with ID list  
Both call `InterruptParsesByIDs` + `PurgeDocumentPipelineTasks` (same multimodal coverage as maintenance stop).

Details: see OpenSpec change artifacts.
