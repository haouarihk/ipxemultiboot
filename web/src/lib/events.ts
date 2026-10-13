import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { api, type BootEvent } from "./api";

/** Recent boot events, kept live through Server-Sent Events. */
export function useLiveEvents(limit = 50) {
  const qc = useQueryClient();
  const initial = useQuery({ queryKey: ["events"], queryFn: api.events });
  const [live, setLive] = useState(false);

  useEffect(() => {
    const es = new EventSource("/api/events/stream");
    es.onopen = () => setLive(true);
    es.onerror = () => setLive(false);
    es.onmessage = (msg) => {
      const e: BootEvent = JSON.parse(msg.data);
      qc.setQueryData<BootEvent[]>(["events"], (old = []) =>
        old.some((x) => x.id === e.id) ? old : [e, ...old].slice(0, 200),
      );
      if (e.kind === "dhcp" || e.kind === "boot" || e.kind === "menu") qc.invalidateQueries({ queryKey: ["clients"] });
      if (e.kind === "iso") qc.invalidateQueries({ queryKey: ["isos"] });
    };
    return () => es.close();
  }, [qc]);

  return { events: (initial.data ?? []).slice(0, limit), live, isLoading: initial.isLoading };
}
