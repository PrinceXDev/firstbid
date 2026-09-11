import React from "react";
import { AbsoluteFill, Sequence } from "remotion";
import { Footage, type Shot } from "./Footage";
import { act, byId, type ActName, FPS } from "../timeline";

export type BeatShot = {
  /** narration beat this shot belongs to */
  id: string;
  shot: Shot;
  dim?: number;
  /** overlays drawn on top; receives the shot and the shot's own length */
  overlay?: (p: { shot: Shot; dur: number }) => React.ReactNode;
};

/** Lays one shot per narration beat, back to back, filling the whole act so
 *  the recording never cuts to black between beats. Each shot's own length is
 *  from its beat's start to the next beat's start (last one runs to act end). */
export const FootageAct: React.FC<{ actName: ActName; shots: BeatShot[] }> = ({
  actName,
  shots,
}) => {
  const A = act(actName);

  return (
    <AbsoluteFill>
      {shots.map((s, i) => {
        const b = byId(s.id);
        // shot starts a touch before the word, so the picture leads the voice
        const startAbs = i === 0 ? A.start : b.start - 0.18;
        const endAbs =
          i === shots.length - 1 ? A.end : byId(shots[i + 1].id).start - 0.18;
        const from = Math.round((startAbs - A.start) * FPS);
        const dur = Math.max(2, Math.round((endAbs - startAbs) * FPS));

        return (
          <Sequence key={s.id} from={from} durationInFrames={dur}>
            <Footage shot={s.shot} durationInFrames={dur} dim={s.dim ?? 0} />
            {s.overlay ? s.overlay({ shot: s.shot, dur }) : null}
          </Sequence>
        );
      })}
    </AbsoluteFill>
  );
};

/** Local seconds of a beat within its act (for timing overlays). */
export const localOf = (actName: ActName, id: string) => {
  const A = act(actName);
  const b = byId(id);
  return { start: b.start - A.start, end: b.end - A.start };
};

/** Local seconds of a beat within ITS OWN shot sequence. */
export const inShot = (shots: BeatShot[], id: string) => {
  const i = shots.findIndex((s) => s.id === id);
  const b = byId(id);
  const startAbs = i === 0 ? undefined : b.start - 0.18;
  return startAbs === undefined ? 0 : 0.18;
};
