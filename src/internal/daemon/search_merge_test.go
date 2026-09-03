package daemon

import (
	"errors"
	"testing"

	"AutoAnimeDownloader/src/internal/anilist"
	"AutoAnimeDownloader/src/internal/nyaa"
)

var errMockNetwork = errors.New("mock network error")

// helper para montar TorrentResult de teste
func mkTorrent(name, magnet string) nyaa.TorrentResult {
	return nyaa.TorrentResult{Name: name, MagnetLink: magnet}
}

// O gate de titulo e aplicado por variante (dentro de ScrapNyaa), entao o que o
// merge testa aqui e que TODAS as variantes sao consultadas e seus resultados
// somados + dedupados — em vez de parar no primeiro retorno nao-vazio.
//
// Clevatess S02E09 (2026-09-03): o release ToonsHub so casava com a variante
// english ("clevatess season 2") porque traz o titulo da temporada 1 no nome.
// Com first-match-wins, a busca parava na variante romaji e ele nunca entrava
// no pool.
func TestSearchNyaaWithVariants_MergesAllVariants(t *testing.T) {
	titles := anilist.Title{
		Romaji:  strPtr("Clevatess II: Majuu no Ou to Itsuwari no Yuusha Denshou"),
		English: strPtr("Clevatess Season 2"),
	}

	resultsByVariant := map[string][]nyaa.TorrentResult{
		"clevatess ii majuu no ou to itsuwari no yuusha denshou":   {mkTorrent("[AnoZu] Clevatess S02E09", "magnet:anozu")},
		"Clevatess II: Majuu no Ou to Itsuwari no Yuusha Denshou":  {mkTorrent("[AnoZu] Clevatess S02E09", "magnet:anozu")},
		"clevatess season 2":                                       {mkTorrent("[ToonsHub] Clevatess S02E09", "magnet:toonshub")},
		"Clevatess Season 2":                                       {mkTorrent("[ToonsHub] Clevatess S02E09", "magnet:toonshub")},
	}

	var queried []string
	searchFn := func(title string) ([]nyaa.TorrentResult, error) {
		queried = append(queried, title)
		return resultsByVariant[title], nil
	}

	got := searchNyaaWithVariants(titles, "", searchFn, "single episode")

	if len(queried) != 4 {
		t.Fatalf("esperava 4 variantes consultadas (todas, mesmo com resultado), obtive %d: %v", len(queried), queried)
	}

	names := map[string]bool{}
	for _, r := range got {
		names[r.Name] = true
	}
	if !names["[AnoZu] Clevatess S02E09"] || !names["[ToonsHub] Clevatess S02E09"] {
		t.Fatalf("esperava resultados das DUAS variantes no pool, obtive %v", names)
	}
}

// Variantes que devolvem resultados sobrepostos (mesmo magnet via multi-pagina)
// nao podem duplicar candidatos no pool.
func TestSearchNyaaWithVariants_DeduplicatesByMagnet(t *testing.T) {
	titles := anilist.Title{
		Romaji:  strPtr("Clevatess II: Majuu no Ou to Itsuwari no Yuusha Denshou"),
		English: strPtr("Clevatess Season 2"),
	}

	shared := mkTorrent("[AnoZu] Clevatess S02E09", "magnet:anozu")
	searchFn := func(title string) ([]nyaa.TorrentResult, error) {
		return []nyaa.TorrentResult{shared, mkTorrent("[AnoZu] Clevatess S02E09 copy", "magnet:anozu")}, nil
	}

	got := searchNyaaWithVariants(titles, "", searchFn, "single episode")

	if len(got) != 1 {
		t.Fatalf("esperava 1 torrent apos dedupe por magnet, obtive %d: %+v", len(got), got)
	}
	if got[0].Name != "[AnoZu] Clevatess S02E09" {
		t.Fatalf("dedupe deve manter a primeira ocorrencia, obteve %s", got[0].Name)
	}
}

// Uma variante com erro nao anula as outras — o merge continua com o que deu certo.
func TestSearchNyaaWithVariants_ErrorInOneVariantDoesNotAbort(t *testing.T) {
	titles := anilist.Title{
		Romaji:  strPtr("Clevatess II: Majuu no Ou to Itsuwari no Yuusha Denshou"),
		English: strPtr("Clevatess Season 2"),
	}

	searchFn := func(title string) ([]nyaa.TorrentResult, error) {
		if title == "clevatess ii majuu no ou to itsuwari no yuusha denshou" {
			return nil, errMockNetwork
		}
		return []nyaa.TorrentResult{mkTorrent("[ToonsHub] Clevatess S02E09", "magnet:toonshub")}, nil
	}

	got := searchNyaaWithVariants(titles, "", searchFn, "single episode")
	if len(got) != 1 || got[0].Name != "[ToonsHub] Clevatess S02E09" {
		t.Fatalf("resultado da variante saudavel deve sobreviver, obtive %+v", got)
	}
}

// Nenhuma variante achou nada: nil, como antes.
func TestSearchNyaaWithVariants_NoResultsReturnsNil(t *testing.T) {
	titles := anilist.Title{
		Romaji:  strPtr("Clevatess II: Majuu no Ou to Itsuwari no Yuusha Denshou"),
		English: strPtr("Clevatess Season 2"),
	}

	searchFn := func(title string) ([]nyaa.TorrentResult, error) { return nil, nil }

	if got := searchNyaaWithVariants(titles, "", searchFn, "single episode"); got != nil {
		t.Fatalf("esperava nil sem resultados, obtive %+v", got)
	}
}

// SearchQueryOverride substitui TODAS as variantes: uma unica query, sem merge.
func TestSearchNyaaWithVariants_CustomQuerySkipsVariants(t *testing.T) {
	titles := anilist.Title{
		Romaji:  strPtr("Clevatess II: Majuu no Ou to Itsuwari no Yuusha Denshou"),
		English: strPtr("Clevatess Season 2"),
	}

	var queried []string
	searchFn := func(title string) ([]nyaa.TorrentResult, error) {
		queried = append(queried, title)
		return []nyaa.TorrentResult{mkTorrent("[X] Clevatess S02E09", "magnet:x")}, nil
	}

	got := searchNyaaWithVariants(titles, "clevatess s2", searchFn, "single episode")

	if len(queried) != 1 || queried[0] != "clevatess s2" {
		t.Fatalf("override deve consultar so a query custom, obteve %v", queried)
	}
	if len(got) != 1 || got[0].Name != "[X] Clevatess S02E09" {
		t.Fatalf("esperava o resultado do override, obtive %+v", got)
	}
}

func strPtr(s string) *string { return &s }
