import { createContext, useContext } from "react";

export type AudioProviderState = {
  /** Whether pick sound effects play (false = muted). Persisted to localStorage. */
  soundEnabled: boolean;
  toggleSound: () => void;
  /** Playback volume, 0..1. Persisted to localStorage. */
  volume: number;
  setVolume: (v: number) => void;
};

const initialState: AudioProviderState = {
  soundEnabled: true,
  toggleSound: () => null,
  volume: 0.5,
  setVolume: () => null,
};

export const AudioProviderContext = createContext<AudioProviderState>(initialState);

export const useAudio = () => useContext(AudioProviderContext);
