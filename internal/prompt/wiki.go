package prompt

// Wiki ingest prompt templates for LLM-powered wiki page generation.

const WikiSummaryPrompt = `You are a wiki editor. Create a structured Markdown wiki summary from the document.

<document>
<title>{{.Title}}</title>
<file_name>{{.FileName}}</file_name>
<file_type>{{.FileType}}</file_type>
<content>
{{.Content}}
</content>
</document>

<available_wiki_pages>
{{.ExtractedSlugs}}
</available_wiki_pages>

<instructions>
1. FIRST line: SUMMARY: {one sentence, 15-40 words — wiki index listing}
2. Then Markdown: key facts/arguments/conclusions; ## / ### headings
3. Wiki-links: available_wiki_pages lists "[[slug]] = display name (Aliases: …)". Match → [[slug|display name]]. Exact slugs only; no inventing; no **bold** or bare [[slug]]
4. Images: if <images>/<image>, include ![caption](url) where relevant
5. End with "## Key Takeaways" bullets
6. Language: {{.Language}}. Length ~500-1500 words by doc size
</instructions>

Output SUMMARY line first, then Markdown. No other preamble.`

const WikiKnowledgeExtractPrompt = `You are a knowledge extraction system. Extract significant entities AND key concepts.

<document>
<title>{{.Title}}</title>
<content>
{{.Content}}
</content>
</document>

<previous_slugs>
{{.PreviousSlugs}}
</previous_slugs>

<instructions>
JSON only: {"entities":[...],"concepts":[...]}. ALL names/descriptions/details in {{.Language}}.

Slug continuity (if previous_slugs): reuse exact slug if still present; omit if gone; add only if genuinely new.

Entities (people/orgs/products/places/tech/events…):
- name ({{.Language}}); slug "entity/<lowercase-hyphenated>" (pinyin if non-Latin)
- aliases: same entity only (abbr/short-full/translation/alt) — not categories; [] if none
- description: 1 sentence, 15-40 words, what it IS + role
- details: 2-5 sentences from doc; images ![caption](url) if relevant
Only substantively discussed (≥2 mentions or detailed). No generics.

Concepts (topics/methods/theories…): same fields; slug "concept/<...>"; aliases=abbr/synonyms not sub-topics.

Dedup: named thing → entities only; abstract → concepts only; never both.
No literal newlines in JSON strings — use \n.
</instructions>

Output ONLY valid JSON. Example:
{
  "entities": [
    {"name":"Acme Corp","slug":"entity/acme-corp","aliases":["Acme"],"description":"A technology company specializing in AI solutions.","details":"Founded in 2020; focuses on enterprise AI."}
  ],
  "concepts": [
    {"name":"Retrieval-Augmented Generation","slug":"concept/retrieval-augmented-generation","aliases":["RAG"],"description":"Combines retrieval with LLM generation.","details":"Retrieve docs, then feed as context to an LLM."}
  ]
}`

const WikiCandidateSlugPrompt = `You are a knowledge extraction system. List significant entities AND concepts as a lightweight candidate set (another pass cites chunks).

<document>
<title>{{.Title}}</title>
<content>
{{.Content}}
</content>
</document>

<previous_slugs>
{{.PreviousSlugs}}
</previous_slugs>

<instructions>
JSON: {"entities":[...],"concepts":[...]}. ALL names/descriptions/details in {{.Language}}.

### Extraction Scope (Granularity: {{.Granularity}})
{{.GranularityGuidance}}

Slug continuity: reuse if still present; omit if gone; new only if genuinely new.

Entities: name, slug "entity/<...>", aliases (same-entity only), description (15-40 words), details (1-3 sentences, <300 chars fallback).
Concepts: same; slug "concept/<...>"; aliases=abbr/synonyms not sub-topics.
Dedup: named→entities; abstract→concepts; never both.
No literal newlines in strings — use \n. Apply Extraction Scope; no trivial name-drops.
</instructions>

Output ONLY valid JSON. Example:
{
  "entities": [
    {"name":"Acme Corp","slug":"entity/acme-corp","aliases":["Acme"],"description":"A technology company specializing in AI solutions.","details":"Founded in 2020; enterprise AI."}
  ],
  "concepts": [
    {"name":"Retrieval-Augmented Generation","slug":"concept/retrieval-augmented-generation","aliases":["RAG"],"description":"Combines retrieval with LLM generation.","details":"Retrieve docs, then feed to an LLM."}
  ]
}`

const WikiChunkCitationPrompt = `You are a precise citation system. For each candidate, list chunk IDs that substantively discuss it.

<document_title>{{.DocTitle}}</document_title>

<candidate_slugs>
{{.CandidateSlugs}}
</candidate_slugs>

<chunks>
{{.ChunksXML}}
</chunks>

<instructions>
ALL names/descriptions/details in {{.Language}}.

Primary: per candidate, cite chunk IDs that substantively discuss it (≥1 concrete fact/attribute/step/date/number/relationship — not a passing mention).
- Only IDs from <chunks> ("id" on <c>, e.g. "c003"); omit if not discussed; one chunk may cite many candidates.

Secondary new_slugs: significant entity/concept NOT in candidates → add with type/name/slug/aliases/description/details + source_chunks. No rediscovery of existing candidates.

JSON only; no literal newlines in strings — use \n.
</instructions>

Format:
{"citations":{"entity/xxx":["c001","c003"],"concept/yyy":["c002"]},"new_slugs":[{"type":"entity","name":"Example","slug":"entity/example","aliases":[],"description":"...","details":"...","source_chunks":["c005"]}]}

Nothing cite-worthy: {"citations": {}, "new_slugs": []}`

const WikiPageModifyPrompt = `You are a wiki editor. Update a page: ADD new info and/or REMOVE stale facts from deleted docs.

<page_metadata>
  <slug>{{.PageSlug}}</slug>
  <title>{{.PageTitle}}</title>
  <type>{{.PageType}}</type>{{if .PageAliases}}
  <aliases>{{.PageAliases}}</aliases>{{end}}
</page_metadata>

Page is about **{{.PageTitle}}** (a {{.PageType}}). Every statement MUST be about this {{.PageType}} — not adjacent/similarly-named things.

<existing_page_content>
{{.ExistingContent}}
</existing_page_content>

{{if .HasAdditions}}
<new_information>
{{.NewContent}}
</new_information>

<new_information> = VERBATIM cited chunks. Optional <source_context> = framing only — do NOT quote into the page.
{{end}}

{{if .HasRetractions}}
<deleted_documents>
{{.DeletedContent}}
</deleted_documents>

<remaining_source_documents>
{{.RemainingSourcesContent}}
</remaining_source_documents>
{{end}}

<valid_wiki_links>
{{.AvailableSlugs}}
</valid_wiki_links>

<instructions>
1. FIRST line: SUMMARY: {one sentence, 15-40 words — wiki index}
{{if .HasRetractions}}
2. REMOVE facts ONLY from deleted docs and absent from remaining/new.
{{end}}
{{if .HasAdditions}}
3. ADD/MERGE from new_information — COMPILER not writer:
   - CONFLICT: reject info about a DIFFERENT thing (e.g. "Hunyuan" vs "Qwen3"; "居民身份证" vs "工作居住证")
   - Same subject + contradicts old → prefer newer
   - Stay close to source wording; light reorder/dedupe OK; no style rewrite/expansion/filler
   - New ##/### only if source or existing has them; flat new page: "# {{.PageTitle}}" + short paras/bullets
   - Self-reported source → attribute, don't elevate to industry claims
{{end}}
4. Keep valid existing content still about {{.PageTitle}}
5. [[slug|name]] only if in valid_wiki_links; never invent; never link own slug {{.PageSlug}}
6. Keep structure; ensure top "# {{.PageTitle}}"
7. Images: ![caption](url) from new info if applicable
{{if .HasRetractions}}
8. Nearly empty after removals + no additions: "SUMMARY: (empty page)\n# {{.PageTitle}}\n\n*This page's primary source document was removed.*"
{{end}}
9. Language: {{.Language}}
</instructions>

SUMMARY line first, then Markdown. No other preamble.`

const WikiIndexIntroPrompt = `You are a wiki editor. Write a brief wiki index introduction.

<document_summaries>
{{.DocumentSummaries}}
</document_summaries>

<instructions>
1. Title: "# " + knowledge domain
2. 2-3 sentences on wiki coverage from summaries
3. Concise header only — directory added separately
4. Language: {{.Language}}
</instructions>

ONLY title + intro. No directory listings or page links.`

const WikiIndexIntroUpdatePrompt = `You are a wiki editor. Update the wiki index introduction for recent changes.

<current_introduction>
{{.ExistingIntro}}
</current_introduction>

<changes>
{{.ChangeDescription}}
</changes>

<document_summaries>
{{.DocumentSummaries}}
</document_summaries>

<instructions>
Reflect current state; mention new topics if scope changes; drop obsolete refs; keep tone/title format; 1 title + 2-3 sentences; Language: {{.Language}}.
</instructions>

ONLY updated title + intro. No directory listings or page links.`

const WikiLogEntryTemplate = `## [{{.Date}}] {{.Operation}} | {{.Title}}
- **Source**: {{.SourceInfo}}
- **Pages affected**: {{.PagesAffected}}
- **Summary**: {{.Summary}}
`

const WikiDeduplicationPrompt = `You are a strict deduplication system. Merge only when new items are the **exact same** real-world entity/concept as an existing page.

<new_items>
{{.NewItems}}
</new_items>

<existing_pages>
{{.ExistingPages}}
</existing_pages>

<instructions>
Merge ONLY if ALL hold:
1. Same real-world thing
2. Name variation only (abbr↔full, translation, minor spelling)
3. Compatible types (entity↔entity or concept↔concept — NEVER cross)

OK: Acme Corp→Acme Corporation; RAG→Retrieval-Augmented Generation; 苹果公司→Apple Inc.
NOT: competing products/versions; subset≠same; related≠same; 居民身份证≠工作居住证; 驾驶证≠行驶证; 学位证≠毕业证.

When in doubt, do NOT merge.
JSON: {"merges":{NEW_slug:EXISTING_slug}} high confidence only; else {"merges":{}}.
No literal newlines in strings.
</instructions>

Output ONLY valid JSON. Example:
{"merges":{"entity/acme-corporation":"entity/acme-corp","concept/rag":"concept/retrieval-augmented-generation"}}`

const (
	WikiGranularityGuidanceFocused = `**FOCUSED — aggressive pruning.**
ONLY primary subjects this document is ABOUT. Aim 3-7 items total.
INCLUDE: main subjects (resume→person+named projects; announcement→org+event/product; product page→product+maker).
EXCLUDE even if named: passing tech stacks; generic methods as detail; background places/schools; one-sentence-only items.
Unsure → LEAVE OUT.`

	WikiGranularityGuidanceStandard = `**STANDARD — balanced (default).**
Main subjects + items with dedicated paragraph, multi-bullet list, or 2-3+ sentences.
INCLUDE: main; secondary with a concrete content block; named methods when HOW is explained.
EXCLUDE: comma-list tech with no detail; one-off/parenthetical/generic infra; one-short-sentence items.
When in doubt, EXCLUDE.`

	WikiGranularityGuidanceExhaustive = `**EXHAUSTIVE — max recall.**
Every named entity/concept/tech/tool/standard/methodology mentioned even once — if concrete/well-known (not "database"/"function").
INCLUDE: main+secondary; named libraries/frameworks/DBs/services/protocols; recognizable concepts (RAG, JWT, …).
EXCLUDE ONLY: truly generic terms; URL-path/citation-only items.
Use for technical glossary KBs.`
)

// WikiGranularityGuidance returns guidance for WikiCandidateSlugPrompt.
// Unknown values → standard.
func WikiGranularityGuidance(granularity string) string {
	switch granularity {
	case "focused":
		return WikiGranularityGuidanceFocused
	case "exhaustive":
		return WikiGranularityGuidanceExhaustive
	default:
		return WikiGranularityGuidanceStandard
	}
}
