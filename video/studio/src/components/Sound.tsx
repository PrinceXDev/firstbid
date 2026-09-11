import React from "react";
import { Audio, Sequence, staticFile, useCurrentFrame, interpolate } from "remotion";
import { BEATS, FPS, TOTAL_SECONDS } from "../timeline";

/** One <Audio> per narration beat, placed at its measured start. */
export const Narration: React.FC = () => (
  <>
    {BEATS.map((b) => (
      <Sequence key={b.id} from={b.from} durationInFrames={b.frames + 6} layout="none">
        <Audio src={staticFile(`vo/${b.id}.mp3`)} volume={1} />
      </Sequence>
    ))}
  </>
);

/** Original synthesised score, ducked under every spoken beat.
 *  The score already carries its own section dynamics (see music.py); this
 *  adds the speech-aware duck so the voice is never fought. */
export const Score: React.FC = () => {
  const speech = React.useMemo(
    () => BEATS.map((b) => [b.start, b.end] as const),
    []
  );

  return (
    <Audio
      src={staticFile("music.wav")}
      volume={(f) => {
        const t = f / FPS;
        // global fade in / out
        const inOut =
          interpolate(t, [0, 2.2], [0, 1], { extrapolateRight: "clamp" }) *
          interpolate(t, [TOTAL_SECONDS - 4.2, TOTAL_SECONDS - 0.2], [1, 0], {
            extrapolateLeft: "clamp",
          });

        // duck: 0.30 under speech, 1.0 in the gaps, with 0.35s ramps
        const R = 0.35;
        let duck = 1;
        for (const [s, e] of speech) {
          if (t >= s - R && t <= e + R) {
            const into = interpolate(t, [s - R, s], [1, 0.3], { extrapolateLeft: "clamp", extrapolateRight: "clamp" });
            const outof = interpolate(t, [e, e + R], [0.3, 1], { extrapolateLeft: "clamp", extrapolateRight: "clamp" });
            duck = Math.min(duck, Math.min(into, outof));
          }
        }
        return 0.5 * inOut * duck;
      }}
    />
  );
};
