import React, { useEffect, useState } from "react";

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
 * Owns the draw-sound on/off preference (mirrored to localStorage by the sound
 * engine) and performs the one-time autoplay unlock. The reel plays the jingle
 * itself (see DrawReel) — this provider just gates it and primes playback so
 * SSE-driven clients, which never click Draw, can still play once the visitor
 * has interacted with the page.
 */
export function AudioProvider({ children }: { children: React.ReactNode }) {
  const [soundEnabled, setEnabled] = useState<boolean>(() => isSoundEnabled());
  const [volume, setVol] = useState<number>(() => getVolume());

  useEffect(() => {
    // Start loading the asset now so the first draw's jingle is ready in time.
    preloadJingle();
    // Unlock on the first user gesture anywhere, then detach — autoplay policy
    // only needs to be satisfied once per session.
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

  const value = {
    soundEnabled,
    toggleSound: () => {
      const next = !soundEnabled;
      setSoundEnabled(next);
      setEnabled(next);
    },
    volume,
    setVolume: (v: number) => {
      applyVolume(v);
      setVol(getVolume()); // read back the clamped, persisted value
    },
  };

  return <AudioProviderContext.Provider value={value}>{children}</AudioProviderContext.Provider>;
}
