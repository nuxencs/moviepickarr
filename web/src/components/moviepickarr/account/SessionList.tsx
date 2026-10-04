import { LogOutIcon, MonitorIcon, SmartphoneIcon, TabletIcon } from "lucide-react";

import { sessionMeta } from "@/components/moviepickarr/account/sessions";

import type { SessionSummary } from "@/types/Response";

/** Matches on the server's label copy ("Safari on iPhone"), so no extra device field. */
export function DeviceIcon({ device }: { device: string }) {
  if (device.includes("iPad")) return <TabletIcon />;
  if (device.includes("iPhone") || device.includes("Android")) return <SmartphoneIcon />;
  return <MonitorIcon />;
}

interface SessionListProps {
  sessions: SessionSummary[];
  revokingID: string | null;
  disabled?: boolean;
  onRevoke: (session: SessionSummary) => void;
}

export function SessionList({ sessions, revokingID, disabled = false, onRevoke }: SessionListProps) {
  return (
    <ul className="acc-devicelist">
      {sessions.map((s) => (
        <li key={s.id} className="acc-device" aria-busy={revokingID === s.id || undefined}>
          <span className="acc-device__icon">
            <DeviceIcon device={s.device} />
          </span>
          <div className="acc-device__text">
            <div className="acc-device__name">{s.device}</div>
            <div className="acc-device__meta">{sessionMeta(s)}</div>
          </div>
          <button
            type="button"
            className="btn btn--ghost btn--sm"
            aria-label={`${revokingID === s.id ? "Signing out of" : "Sign out of"} ${s.device}`}
            onClick={() => onRevoke(s)}
            disabled={disabled || revokingID !== null}
          >
            <LogOutIcon aria-hidden="true" />
            {revokingID === s.id ? "Signing out…" : "Sign out"}
          </button>
        </li>
      ))}
    </ul>
  );
}
