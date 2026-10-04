import React, { useCallback, useEffect, useMemo, useState } from "react";

import { AudioProviderContext } from "@/components/audio-context";

import {
  getVolume,
  isSoundEnabled,
  preloadJingle,
  setSoundEnabled,
  setVolume as applyVolume,
  unlockAudio,
} from "@/lib/sound";

/**
 * Gates the draw sound and does the one-time autoplay unlock, so SSE-driven
 * clients that never click Draw can still play the jingle (DrawReel plays it).
 */
export function AudioProvider({ children }: { children: React.ReactNode }) {
  const [soundEnabled, setEnabled] = useState<boolean>(() => isSoundEnabled());
  const [volume, setVol] = useState<number>(() => getVolume());

  useEffect(() => {
    preloadJingle();
    // Autoplay policy needs one user gesture per session.
    const unlock = () => {
      unlockAudio();
      window.removeEventListener("pointerdown", unlock);
      window.removeEventListener("keydown", unlock);
    };
    window.addEventListener("pointerdown", unlock);
    window.addEventListener("keydown", unlock);
    return () => {
      window.removeEventListener("pointerdown", unlock);
      window.removeEventListener("keydown", unlock);
    };
  }, []);

  const toggleSound = useCallback(() => {
    const next = !soundEnabled;
    setSoundEnabled(next);
    setEnabled(next);
  }, [soundEnabled]);

  const setVolume = useCallback((v: number) => {
    applyVolume(v);
    setVol(getVolume()); // read back the clamped, persisted value
  }, []);

  // Stable identity, or every useAudio() consumer re-renders per slider drag tick.
  const value = useMemo(
    () => ({ soundEnabled, toggleSound, volume, setVolume }),
    [soundEnabled, toggleSound, volume, setVolume],
  );

  return <AudioProviderContext.Provider value={value}>{children}</AudioProviderContext.Provider>;
}
