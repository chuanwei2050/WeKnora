package types

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeQualificationAliases(t *testing.T) {
	got, err := NormalizeQualificationAliases(QualificationAliasMappings{{Alias: " 建工一级 ", StandardName: " 建筑工程施工总承包一级资质 "}})
	require.NoError(t, err)
	require.Equal(t, QualificationAliasMappings{{Alias: "建工一级", StandardName: "建筑工程施工总承包一级资质"}}, got)

	_, err = NormalizeQualificationAliases(QualificationAliasMappings{{Alias: "ABC", StandardName: "一"}, {Alias: " abc ", StandardName: "二"}})
	require.EqualError(t, err, `qualification_aliases contains duplicate alias "abc"`)
}

func TestExpandQueryWithQualificationAliases(t *testing.T) {
	mappings := QualificationAliasMappings{
		{Alias: "建工", StandardName: "短名称"},
		{Alias: "建工一级", StandardName: "建筑工程施工总承包一级资质"},
		{Alias: "一级", StandardName: "建筑工程施工总承包一级资质"},
	}
	require.Equal(t, "没有简称", ExpandQueryWithQualificationAliases("没有简称", mappings))
	require.Equal(t, "建工一级怎么办 建筑工程施工总承包一级资质", ExpandQueryWithQualificationAliases("建工一级怎么办", mappings))
}

func TestExpandQueryWithQualificationAliasesKeepsDistinctNamesForSameAliasAcrossKnowledgeBases(t *testing.T) {
	first := QualificationAliasMappings{{Alias: "建工", StandardName: "标准名称A"}}
	second := QualificationAliasMappings{{Alias: "建工", StandardName: "标准名称B"}}

	require.Equal(t, "建工 标准名称A 标准名称B", ExpandQueryWithQualificationAliases("建工", first, second))
}

func TestExpandQueryWithQualificationAliasesBoundsOutput(t *testing.T) {
	query := strings.Repeat("原", MaxExpandedQueryBytes/len([]byte("原")))
	got := ExpandQueryWithQualificationAliases(query+"简称", QualificationAliasMappings{{Alias: "简称", StandardName: strings.Repeat("名", 100)}})
	require.Equal(t, strings.TrimSpace(query+"简称"), got)
}
