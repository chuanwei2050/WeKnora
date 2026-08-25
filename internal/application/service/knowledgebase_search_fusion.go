package service

import (
	"context"

	"slices"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// classifyRetrievalResults separates retrieval results by retriever type (vector vs keyword).
func classifyRetrievalResults(ctx context.Context, retrieveResults []*types.RetrieveResult) (
	vectorResults, keywordResults []*types.IndexWithScore,
) {
	for _, retrieveResult := range retrieveResults {
		logger.Infof(ctx, "Retrieval results, engine: %v, retriever: %v, count: %v",
			retrieveResult.RetrieverEngineType,
			retrieveResult.RetrieverType,
			len(retrieveResult.Results),
		)
		if retrieveResult.RetrieverType == types.VectorRetrieverType {
			vectorResults = append(vectorResults, retrieveResult.Results...)
		} else {
			keywordResults = append(keywordResults, retrieveResult.Results...)
		}
	}
	return
}

// fuseOrDeduplicate either fuses vector+keyword results via RRF or deduplicates vector-only results.
func fuseOrDeduplicate(
	ctx context.Context,
	vectorResults, keywordResults []*types.IndexWithScore,
	vectorWeight float64,
) []*types.IndexWithScore {
	if len(keywordResults) == 0 {
		// Vector-only: keep original embedding scores (important for FAQ)
		result := deduplicateByScore(vectorResults)
		logger.Infof(ctx, "Result count after deduplication: %d", len(result))
		return result
	}
	if len(vectorResults) == 0 {
		// Keyword-only: keep original scores (important for FAQ)
		result := deduplicateByScore(keywordResults)
		logger.Infof(ctx, "Result count after deduplication: %d", len(result))
		return result
	}
	// Hybrid: use RRF fusion to merge vector + keyword results
	result := fuseWithRRF(ctx, vectorResults, keywordResults, vectorWeight)
	logger.Infof(ctx, "Result count after RRF fusion: %d", len(result))
	return result
}

// sortByScoreDesc is a reusable sort comparator for IndexWithScore slices (descending by Score).
func sortByScoreDesc(a, b *types.IndexWithScore) int {
	if a.Score > b.Score {
		return -1
	} else if a.Score < b.Score {
		return 1
	}
	return 0
}

// deduplicateByScore deduplicates retrieval results by chunk ID, keeping the highest score
// for each chunk. Returns the results sorted by score descending.
// Used when only a single retriever (e.g. vector-only for FAQ) is active.
func deduplicateByScore(results []*types.IndexWithScore) []*types.IndexWithScore {
	chunkInfoMap := make(map[string]*types.IndexWithScore, len(results))
	for _, r := range results {
		if existing, exists := chunkInfoMap[r.ChunkID]; !exists || r.Score > existing.Score {
			chunkInfoMap[r.ChunkID] = r
		}
	}
	deduped := make([]*types.IndexWithScore, 0, len(chunkInfoMap))
	for _, info := range chunkInfoMap {
		deduped = append(deduped, info)
	}
	slices.SortFunc(deduped, sortByScoreDesc)
	return deduped
}

// fuseWithRRF merges vector and keyword retrieval results using Reciprocal Rank Fusion.
// RRF score = vectorWeight/(k+vectorRank) + keywordWeight/(k+keywordRank), with k=60.
// The merged results are sorted by RRF score descending.
func fuseWithRRF(
	ctx context.Context,
	vectorResults, keywordResults []*types.IndexWithScore,
	vectorWeight float64,
) []*types.IndexWithScore {
	const rrfK = 60
	if vectorWeight <= 0 || vectorWeight >= 1 {
		vectorWeight = 0.7
	}
	keywordWeight := 1 - vectorWeight

	// Build rank maps for each retriever (already sorted by score from retriever)
	vectorRanks := make(map[string]int, len(vectorResults))
	for i, r := range vectorResults {
		if _, exists := vectorRanks[r.ChunkID]; !exists {
			vectorRanks[r.ChunkID] = i + 1 // 1-indexed rank
		}
	}
	keywordRanks := make(map[string]int, len(keywordResults))
	for i, r := range keywordResults {
		if _, exists := keywordRanks[r.ChunkID]; !exists {
			keywordRanks[r.ChunkID] = i + 1
		}
	}

	// Collect all unique chunks — prefer vector result's metadata for each chunk
	chunkInfoMap := make(map[string]*types.IndexWithScore)
	for _, r := range vectorResults {
		if existing, exists := chunkInfoMap[r.ChunkID]; !exists || r.Score > existing.Score {
			chunkInfoMap[r.ChunkID] = r
		}
	}
	for _, r := range keywordResults {
		if _, exists := chunkInfoMap[r.ChunkID]; !exists {
			chunkInfoMap[r.ChunkID] = r
		}
	}

	// Compute weighted RRF scores and assign to each chunk
	result := make([]*types.IndexWithScore, 0, len(chunkInfoMap))
	for chunkID, info := range chunkInfoMap {
		rrfScore := 0.0
		if rank, ok := vectorRanks[chunkID]; ok {
			rrfScore += vectorWeight / float64(rrfK+rank)
		}
		if rank, ok := keywordRanks[chunkID]; ok {
			rrfScore += keywordWeight / float64(rrfK+rank)
		}
		// Keep the retrievers' original scores intact. Their ranked lists are
		// reused below to preserve strong single-channel candidates for rerank.
		fusedInfo := *info
		fusedInfo.Score = rrfScore
		result = append(result, &fusedInfo)
	}
	slices.SortFunc(result, sortByScoreDesc)

	// Log top results for debugging
	for i, chunk := range result {
		if i >= 15 {
			break
		}
		vRank, vOk := vectorRanks[chunk.ChunkID]
		kRank, kOk := keywordRanks[chunk.ChunkID]
		logger.Debugf(ctx, "RRF rank %d: chunk_id=%s, rrf_score=%.6f, vector_rank=%v(%v), keyword_rank=%v(%v)",
			i, chunk.ChunkID, chunk.Score, vRank, vOk, kRank, kOk)
	}

	return result
}

// preserveRetrieverLeaders keeps the strongest candidates from both retrieval
// channels near the front of the list so the downstream reranker can evaluate
// them. RRF alone can otherwise bury a high-ranked single-channel match behind
// weaker chunks that happen to occur in both channels.
func preserveRetrieverLeaders(
	fused, vectorResults, keywordResults []*types.IndexWithScore,
	limit int,
) []*types.IndexWithScore {
	if limit <= 0 || len(fused) <= limit {
		return fused
	}

	leadersPerChannel := min(5, max(1, limit/3))
	result := make([]*types.IndexWithScore, 0, limit)
	seen := make(map[string]struct{}, limit)
	appendUnique := func(candidate *types.IndexWithScore) {
		if candidate == nil || len(result) >= limit {
			return
		}
		if _, exists := seen[candidate.ChunkID]; exists {
			return
		}
		seen[candidate.ChunkID] = struct{}{}
		result = append(result, candidate)
	}

	for i := 0; i < leadersPerChannel; i++ {
		if i < len(vectorResults) {
			appendUnique(vectorResults[i])
		}
		if i < len(keywordResults) {
			appendUnique(keywordResults[i])
		}
	}
	for _, candidate := range fused {
		appendUnique(candidate)
	}

	return result
}
