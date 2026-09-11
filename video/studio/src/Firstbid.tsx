import React from "react";
import { AbsoluteFill, Sequence, useCurrentFrame, interpolate } from "remotion";
import { C, F } from "./theme";
import { ACTS, FPS, TOTAL_SECONDS, type ActName } from "./timeline";
import { Narration, Score } from "./components/Sound";
import { Captions } from "./components/Captions";

import { Problem } from "./scenes/Problem";
import { Title } from "./scenes/Title";
import { Read } from "./scenes/Read";
import { Refusal } from "./scenes/Refusal";
import { Model } from "./scenes/Model";
import { Evidence } from "./scenes/Evidence";
import { Autopsy } from "./scenes/Autopsy";
import { Coverage } from "./scenes/Coverage";
import { Architecture } from "./scenes/Architecture";
import { Mechanism } from "./scenes/Mechanism";
import { Live } from "./scenes/Live";
import { Pitch } from "./scenes/Pitch";

const SCENES: Record<ActName, React.FC> = {
  PROBLEM: Problem,
  TITLE: Title,
  READ: Read,
  REFUSAL: Refusal,
  MODEL: Model,
  EVIDENCE: Evidence,
  AUTOPSY: Autopsy,
  COVERAGE: Coverage,
  ARCHITECTURE: Architecture,
  MECHANISM: Mechanism,
  LIVE: Live,
  PITCH: Pitch,
};

/** Chapter label, top left. Names the act without stealing attention. */
const CHAPTERS: Partial<Record<ActName, string>> = {
  READ: "01 · THE READ",
  REFUSAL: "02 · THE REFUSAL",
  MODEL: "03 · THE MODEL",
  EVIDENCE: "04 · THE EVIDENCE",
  AUTOPSY: "05 · THE CORRECTION",
  COVERAGE: "06 · WHAT IT REFUSES",
  ARCHITECTURE: "07 · THE ENGINE",
  MECHANISM: "08 · THE MECHANISM",
  LIVE: "09 · LIVE",
};

const Chapter: React.FC<{ label: string; frames: number }> = ({ label, frames }) => {
  const f = useCurrentFrame();
  const a =
    interpolate(f, [4, 18], [0, 1], { extrapolateLeft: "clamp", extrapolateRight: "clamp" }) *
    interpolate(f, [frames - 16, frames - 4], [1, 0], { extrapolateLeft: "clamp", extrapolateRight: "clamp" });
  return (
    <div
      style={{
        position: "absolute",
        left: 38,
        top: 15,
        opacity: a * 0.9,
        fontFamily: F.mono,
        fontSize: 18,
        letterSpacing: 3.6,
        color: C.inkFaint,
        textTransform: "uppercase",
        pointerEvents: "none",
      }}
    >
      {label}
    </div>
  );
};

/** A short dip to black between acts, so cuts never feel abrupt. */
const ActDip: React.FC<{ frames: number }> = ({ frames }) => {
  const f = useCurrentFrame();
  const inDip = interpolate(f, [0, 7], [1, 0], { extrapolateLeft: "clamp", extrapolateRight: "clamp" });
  const outDip = interpolate(f, [frames - 7, frames], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });
  return (
    <AbsoluteFill
      style={{ background: "#000", opacity: Math.max(inDip, outDip) * 0.85, pointerEvents: "none" }}
    />
  );
};

export const Firstbid: React.FC = () => {
  const f = useCurrentFrame();
  const t = f / FPS;

  // open from black, close to black
  const open = interpolate(t, [0, 1.1], [1, 0], { extrapolateLeft: "clamp", extrapolateRight: "clamp" });
  const close = interpolate(t, [TOTAL_SECONDS - 1.5, TOTAL_SECONDS - 0.1], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  return (
    <AbsoluteFill style={{ backgroundColor: C.bg }}>
      {ACTS.map((a) => {
        const Scene = SCENES[a.name];
        const chapter = CHAPTERS[a.name];
        return (
          <Sequence key={a.name} from={a.from} durationInFrames={a.frames} name={a.name}>
            <Scene />
            {chapter ? <Chapter label={chapter} frames={a.frames} /> : null}
            <ActDip frames={a.frames} />
          </Sequence>
        );
      })}

      <Captions />

      {/* a fine top/bottom rule frames the picture without a heavy letterbox */}
      <AbsoluteFill style={{ pointerEvents: "none" }}>
        <div
          style={{
            position: "absolute",
            left: 0, right: 0, top: 47,
            height: 1,
            background: "rgba(126,156,255,0.10)",
          }}
        />
        <div
          style={{
            position: "absolute",
            left: 0, right: 0, top: 984,
            height: 1,
            background: "rgba(126,156,255,0.10)",
          }}
        />
      </AbsoluteFill>

      <Narration />
      <Score />

      <AbsoluteFill
        style={{ background: "#000", opacity: Math.max(open, close), pointerEvents: "none" }}
      />
    </AbsoluteFill>
  );
};
