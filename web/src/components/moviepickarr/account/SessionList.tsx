import { MonitorIcon, SmartphoneIcon, TabletIcon } from "lucide-react";

import { sessionMeta } from "@/components/moviepickarr/account/sessions";

import type { SessionSummary } from "@/types/Response";

/** The device family behind a label, for the row icon. The label is server-side
 *  copy ("Safari on iPhone"), so matching on it keeps the shape rule in one
 *  place rather than shipping a second device field just to pick a glyph. */
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

/**
 * The member's other signed-in devices, revealed only when they choose to
 * manage them. One flat register with dividers keeps the management view
 * scannable without turning every device into another settings card.
 */
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
            {revokingID === s.id ? "Signing out…" : "Sign out"}
          </button>
        </li>
      ))}
    </ul>
  );
}
