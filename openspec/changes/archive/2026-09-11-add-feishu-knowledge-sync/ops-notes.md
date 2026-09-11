# Feishu Knowledge Sync — Ops Notes

用户侧配置步骤见仓库文档：[docs/飞书知识库同步.md](../../../docs/飞书知识库同步.md)。

## Feature flag

- Env: `ENABLE_FEISHU_KNOWLEDGE_SYNC=true|false` (default **false**)
- When disabled:
  - Config/discovery/preview/confirm APIs return feature-disabled
  - Queued workers skip execution
  - Existing Feishu wiki pages and WeKnora mappings are **not** deleted or modified

## Enable checklist

1. Set `SYSTEM_AES_KEY` (32 bytes) so `app_secret` is encrypted at rest.
2. Set `ENABLE_FEISHU_KNOWLEDGE_SYNC=true` on app workers (API + asynq/lite executor).
3. Create a Feishu enterprise self-built app with at least:
   - `wiki:wiki` / `wiki:node:create` (or equivalent write scopes)
   - Docx edit + drive media upload scopes used by File Block
4. Authorize the app on the target wiki:
   - Preferred: enable **Bot** → publish → in wiki **成员设置** search the **app name** and add as admin/member
     (without Bot, member search usually cannot find the app).
   - Fallback: add app as group bot → add that **group** as wiki member.
   - Note: page-level **添加文档应用** alone does **not** make the space appear in discover.
5. In KB settings → **同步飞书**, fetch spaces, pick location, preview, then confirm.

## Live tenant progress (2026-09-10 / 2026-09-11)

- App `cli_aa281007c0f99bcf`（知识库）published through **1.0.2** (Bot + `docs:document.media:upload`).
- Wiki「WeKnora测试知识库」(`7683904691322784699`): app added as **管理员** via member picker.
- WeKnora: `ENABLE_FEISHU_KNOWLEDGE_SYNC=true`; discover / preview / confirm exercised on KB「待审查文档」(`a8780653-…`).
- Runtime fixes validated live:
  1. Create File Block returns View (type 33); replace must target `view.children[0]` File Block — fixed `resolveCreatedFileBlock`.
  2. Failed-run same-digest retry hit snapshot unique key — `CreateSnapshot` now reuses digest; Confirm resets terminal run and re-enqueues.
  3. Wiki rename must use `POST .../nodes/:token/update_title` (not `PUT .../nodes/:token`) — fixed `UpdateWikiNodeTitle`.
- Acceptance runs on the same KB:
  - First full sync `299e2558-…` → `succeeded` (~120s, ~53.4 MB, create=41 / skip=2).
  - No-change re-run `779371ab-…` → `succeeded` (~0.4s, skip=43, upload_bytes=0).
  - `replace_content` succeeded; first rename failed on wrong API then succeeded after fix (`379be493-…`).
  - Tombstone `a5b9f2f3-…` → `succeeded`: deleted source → mapping `archived`, title `[已归档] …`, parent `_KnowledgeMesh归档`.
  - Restore `304a3ab6-…` → `succeeded` (after recreating MinIO object removed by soft-delete): mapping `active`, title unprefixed, parent back under managed home.
  - Directory move `49627d41-…` → `succeeded`: created wiki dir `FeishuMoveTest`, moved restored doc under it (`parent_node_token` = dir node).
- Link stability: node_token URLs still open after rename/retire/restore/move.
- Home: `https://feishu.cn/wiki/EHI9wPToii4urZkQdyvcfumEnug`
- Security: App Secret was revealed during headed automation — **rotate App Secret** after联调.

## Product tips (1.2 / search / attachments)

- Feishu ordinary search / 知识问答 indexing is **eventually consistent**; do **not** promise a hit latency SLA after sync or tombstone. Product copy should say “同步后稍后可搜”，not a fixed minutes number.
- Page links keyed by wiki `node_token` remained stable across content replace, rename, archive, restore, move, and recreate in this tenant.
- Soft-deleting knowledge in WeKnora may remove the MinIO object; restore sync needs the object (or a re-upload) or it fails with “specified key does not exist”.
- Attachment parity: WeKnora MinIO object MD5/size matched DB `file_hash`/`file_size` on spot-check (62 B sample). Feishu `medias/.../download` needs extra Drive/Docs download scopes beyond the minimal publish set — **not** part of the default publish permission matrix; UI spot-check remains the operator fallback.

## Perf & structure snapshot (7.4 revised)

| Scenario | Docs / bytes | Duration |
| --- | --- | --- |
| First full create | 41 docs / ~53.4 MB | ~120 s |
| No-change re-run | 43 skips / 0 B | ~0.4 s |
| Single rename | 1 update | ~1 s |
| Single retire | 1 retire | ~4 s |
| Single restore | 1 restore / 62 B | ~few s |
| Dir create + move | 1 create + 1 move | ~few s |
| Conflict overwrite (status=conflict → replace_content) | 1 update | ~4 s |
| Remote rename+move overwrite (live Feishu drift → refresh → rename+move) | 1 update + 1 move | ~preview 15 s + run ~few s |
| Missing node recreate (poisoned node_token → replace/create) | 1 update | ~few s |
| Deep directory tree DeepL1…DeepL5 | 5 directory creates | ~22 s |
| Duplicate display names under DeepL5 | 2 creates, distinct node_tokens | ~28 s |

**Revised gate (not ≥100 docs):** baseline ≥40 docs / ~50MB + ≥4-level directory tree + same title / different IDs create separate pages. ≥100-doc stress and empty-dir / multi-GB file soak remain out of scope for this change.

## Still open (explicitly deferred)

- ≥100-document / multi-GB soak performance.
- Feishu search/QA latency quantification (product tip only).
- Feishu media byte download automation (requires extra OAuth scopes).

## Conflict policy (manual overwrite)

- Sync is **manual one-click only** (preview → confirm). No schedule/Webhook.
- Preview/execute **refresh live Feishu title/parent** into in-memory mappings before planning, so remote rename/move is overwritten even when DB mapping still looked healthy.
- On remote drift (rename/move/missing managed node), the next manual sync **overwrites** from WeKnora: restore title/parent, rewrite managed blocks, or recreate the page if the node is gone.
- Live checks 2026-09-11:
  - Forced mapping `conflict` → `replace_content` → `active`.
  - Feishu rename+move under archive → preview `rename,move` → `succeeded`.
  - Poisoned `node_token` → `replace_content`/`recreate` → new `node_token` active.
- Non-managed user notes on the same Feishu page are still not deleted.
- Official Wiki **delete-node** REST API is unavailable; “删页后再同步”验收用 **节点不可达/伪造 token** 等价路径。

## Observability

- Structured logs use prefix `[FeishuPublish]` and Feishu client `[Feishu]`.
- Do not log secrets, tokens, file contents, or full Feishu response bodies (sanitizer applied).
- Run statuses: `queued` → `running` → `succeeded` | `partial` | `failed`.

## Rollback

1. Set `ENABLE_FEISHU_KNOWLEDGE_SYNC=false`.
2. Stop consuming new publish tasks.
3. Leave mappings/runs/Feishu pages intact for later re-enable.
