import React from "react";
import { Composition } from "remotion";
import { loadFont as loadInter } from "@remotion/google-fonts/Inter";
import { loadFont as loadMono } from "@remotion/google-fonts/JetBrainsMono";
import { Firstbid } from "./Firstbid";
import { FPS, W, H, TOTAL_FRAMES } from "./timeline";

loadInter("normal", { weights: ["400", "500", "600", "700", "800"], subsets: ["latin"] });
loadMono("normal", { weights: ["400", "700"], subsets: ["latin"] });

export const RemotionRoot: React.FC = () => (
  <>
    <Composition
      id="Firstbid"
      component={Firstbid}
      durationInFrames={TOTAL_FRAMES}
      fps={FPS}
      width={W}
      height={H}
    />
  </>
);
