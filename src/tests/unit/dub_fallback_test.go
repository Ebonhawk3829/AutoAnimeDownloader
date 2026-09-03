package unit

import (
	"AutoAnimeDownloader/src/internal/nyaa"
	"testing"
)

// Clevatess S02E09 (2026-09-03): "[Yameii] Clevatess - S02E09 v2 [English Dub] ..."
// casava com o token de fallback "dub" (índice 10) e vencia AnoZu/VARYG, que são
// grupos desconhecidos (len = 12 = pior). O release dublado não pode ganhar de um
// grupo que nem está na lista: desconhecido > tokens de fallback (dub, raw).
func TestFansubPriority_UnknownGroupBeatsDubFallback(t *testing.T) {
	defer nyaa.SetPriorities(nyaa.DefaultPriorities())()

	dub := "[Yameii] Clevatess - S02E09 v2 [English Dub] [CR WEB-DL 1080p H264 AAC]"
	unknown := "[AnoZu] Clevatess S02E09 REPACK 1080p CR WEB-DL Dual-Audio DDP 2.0 H.264"

	if nyaa.FansubPriorityForTest(dub) <= nyaa.FansubPriorityForTest(unknown) {
		t.Fatalf("dub (fallback) deve pontuar pior que grupo desconhecido: dub=%d unknown=%d",
			nyaa.FansubPriorityForTest(dub), nyaa.FansubPriorityForTest(unknown))
	}
}

// A ordenação ponta a ponta com os nomes reais do incidente: o dub 1080p não pode
// vencer os releases dual-audio/subbed 1080p na mesma faixa de saúde.
func TestSortTorrentResults_ClevatessDubDoesNotWin(t *testing.T) {
	defer nyaa.SetPriorities(nyaa.DefaultPriorities())()

	results := []nyaa.TorrentResult{
		{Name: "[AnoZu] Clevatess S02E09 REPACK 1080p CR WEB-DL Dual-Audio DDP 2.0 H.264", Seeders: "300"},
		{Name: "[Yameii] Clevatess - S02E09 v2 [English Dub] [CR WEB-DL 1080p H264 AAC] [5BC607B3]", Seeders: "300"},
		{Name: "Clevatess S02E09 The Mirror Labyrinth 1080p CR WEB-DL DUAL AAC2.0 H.264-VARYG", Seeders: "300"},
	}

	sorted := nyaa.SortTorrentResults(results)
	for _, r := range sorted {
		t.Logf("sorted: %s", r.Name)
	}
	if sorted[0].Name != results[0].Name {
		t.Fatalf("esperava AnoZu (subbed) no topo, obteve %s", sorted[0].Name)
	}
}

// Grupo desconhecido não perde mais para o fallback, mas grupos listados continuam
// na frente de ambos — o fallback só afeta quem casaria com "dub"/"raw".
func TestFansubPriority_ListedGroupStillBeatsUnknown(t *testing.T) {
	defer nyaa.SetPriorities(nyaa.DefaultPriorities())()

	listed := "[SubsPlease] Anime - 01 1080p"
	unknown := "[RandomGroup] Anime - 01 1080p"

	if nyaa.FansubPriorityForTest(listed) >= nyaa.FansubPriorityForTest(unknown) {
		t.Fatalf("grupo listado deve vencer desconhecido: listed=%d unknown=%d",
			nyaa.FansubPriorityForTest(listed), nyaa.FansubPriorityForTest(unknown))
	}
}

// Dois desconhecidos empatam entre si (mesmo score) — critério seguinte decide.
func TestFansubPriority_UnknownGroupsTie(t *testing.T) {
	defer nyaa.SetPriorities(nyaa.DefaultPriorities())()

	a := "[AnoZu] Anime - 01 1080p"
	b := "[VARYG] Anime - 01 1080p"
	if nyaa.FansubPriorityForTest(a) != nyaa.FansubPriorityForTest(b) {
		t.Fatalf("grupos desconhecidos devem empatar: %d vs %d",
			nyaa.FansubPriorityForTest(a), nyaa.FansubPriorityForTest(b))
	}
}

// O default da IgnoreList pega "[English Dub]" sem colchete antes do "dub".
func TestIgnoreList_DefaultCatchesEnglishDub(t *testing.T) {
	defer nyaa.SetPriorities(nyaa.DefaultPriorities())()

	if !nyaa.ShouldIgnore("[Yameii] Clevatess - S02E09 v2 [English Dub] [CR WEB-DL 1080p H264 AAC]") {
		t.Fatal("default ignore_list deve descartar '[English Dub]'")
	}
	if !nyaa.ShouldIgnore("[Group] Anime [Dub] 1080p") {
		t.Fatal("[dub] literal continua filtrando")
	}
	// Nada de pegar releases subbed normais.
	if nyaa.ShouldIgnore("[SubsPlease] Anime - 01 1080p") {
		t.Fatal("release subbed não deve ser ignorado")
	}
}
