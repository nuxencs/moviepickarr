import { ChevronRightIcon } from "lucide-react";
import { useId, useState } from "react";

import type { ReactNode } from "react";

interface RadarrDisclosureProps {
  children: ReactNode;
  className?: string;
  defaultOpen?: boolean;
  meta?: ReactNode;
  title: ReactNode;
}

/** One accessible, animated disclosure language for the Radarr workspace. */
export function RadarrDisclosure({
  children,
  className,
  defaultOpen = false,
  meta,
  title,
}: RadarrDisclosureProps) {
  const [open, setOpen] = useState(defaultOpen);
  const baseID = useId();
  const triggerID = `${baseID}-trigger`;
  const labelID = `${baseID}-label`;
  const contentID = `${baseID}-content`;
  const metaID = `${baseID}-meta`;
  const classes = ["radarr-disclosure", className].filter(Boolean).join(" ");

  return (
    <section className={classes} data-open={open}>
      <button
        id={triggerID}
        type="button"
        className="radarr-disclosure__trigger"
        aria-expanded={open}
        aria-controls={contentID}
        aria-labelledby={labelID}
        aria-describedby={meta !== undefined && meta !== null ? metaID : undefined}
        onClick={() => setOpen((current) => !current)}
      >
        <span id={labelID} className="radarr-disclosure__label">{title}</span>
        <span className="radarr-disclosure__end">
          {meta !== undefined && meta !== null ? (
            <span id={metaID} className="radarr-disclosure__meta">{meta}</span>
          ) : null}
          <ChevronRightIcon className="radarr-disclosure__chevron" aria-hidden="true" />
        </span>
      </button>
      <div className="radarr-disclosure__viewport" aria-hidden={!open} inert={!open}>
        <div className="radarr-disclosure__inner">
          <div
            id={contentID}
            className="radarr-disclosure__content"
            role="region"
            aria-labelledby={labelID}
          >
            {children}
          </div>
        </div>
      </div>
    </section>
  );
}
