import type { Priorities } from "../api/client.js"

/**
 * Preset é carimbo de uma vez, não modo guardado: aplica, vira lista comum e editável,
 * e o botão "Salvar" que já existe persiste. Nada novo em config.json.
 *
 * Reordena em vez de carimbar um array literal — só promove tokens que o usuário já tem.
 * Assim um token adicionado à mão desce em vez de sumir, e os tokens canônicos continuam
 * existindo num lugar só (reCodecPatterns, no backend).
 */
export type ListPreset = { key: string; label: string; desc: string; first: string[] }

export const PRESETS: Partial<Record<keyof Priorities, ListPreset[]>> = {
  codecs: [
    {
      key: "compat",
      label: "Prefer compatibility",
      desc: "H.264 first. Plays directly in any player, no server transcoding — subtitles stay soft. Larger files.",
      first: ["h.264"],
    },
    {
      key: "space",
      label: "Prefer smaller files",
      desc: "AV1/HEVC first. Up to half the size at the same quality, but requires a player that decodes it — the browser transcodes.",
      first: ["av1", "hevc"],
    },
  ],
}

/** Devolve `list` com os itens de `first` que ela contém promovidos ao topo, na ordem de `first`. */
export function applyPreset(list: string[], first: string[]): string[] {
  const promote = first.filter((v) => list.includes(v))
  return [...promote, ...list.filter((v) => !promote.includes(v))]
}
