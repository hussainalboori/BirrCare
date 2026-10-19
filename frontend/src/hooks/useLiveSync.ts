"use client";

import { useEffect, useState, useRef, useCallback } from "react";
import { SSEEvent } from "@/lib/types";

export type ConnectionStatus = "connected" | "connecting" | "disconnected";

interface UseLiveSyncOptions {
  onEvent?: (event: SSEEvent) => void;
  onMedLogged?: (event: SSEEvent) => void;
  onVitalLogged?: (event: SSEEvent) => void;
  onNoteAdded?: (event: SSEEvent) => void;
  onCaregiverSwitched?: (event: SSEEvent) => void;
}

export function useLiveSync(options: UseLiveSyncOptions = {}) {
  const [status, setStatus] = useState<ConnectionStatus>("connecting");
  const [lastEvent, setLastEvent] = useState<SSEEvent | null>(null);
  const eventSourceRef = useRef<EventSource | null>(null);
  const reconnectTimeoutRef = useRef<NodeJS.Timeout | null>(null);
  const optionsRef = useRef(options);
  optionsRef.current = options;

  const connect = useCallback(() => {
    if (typeof window === "undefined") return;

    if (eventSourceRef.current) {
      eventSourceRef.current.close();
    }

    const apiUrl = process.env.NEXT_PUBLIC_API_URL || "/api/v1";
    const streamUrl = `${apiUrl}/events/stream`;

    setStatus("connecting");
    const es = new EventSource(streamUrl, { withCredentials: true });
    eventSourceRef.current = es;

    es.onopen = () => {
      setStatus("connected");
    };

    es.addEventListener("connected", () => {
      setStatus("connected");
    });

    es.onmessage = (event) => {
      try {
        const parsed: SSEEvent = JSON.parse(event.data);
        setLastEvent(parsed);

        // Global callback
        if (optionsRef.current.onEvent) {
          optionsRef.current.onEvent(parsed);
        }

        // Specific callbacks
        switch (parsed.type) {
          case "MED_LOGGED":
            optionsRef.current.onMedLogged?.(parsed);
            break;
          case "VITAL_LOGGED":
            optionsRef.current.onVitalLogged?.(parsed);
            break;
          case "NOTE_ADDED":
            optionsRef.current.onNoteAdded?.(parsed);
            break;
          case "CAREGIVER_SWITCHED":
            optionsRef.current.onCaregiverSwitched?.(parsed);
            break;
        }

        // Dispatch browser window event for toasts
        window.dispatchEvent(new CustomEvent("birrcare:sse_event", { detail: parsed }));
      } catch (err) {
        console.warn("[SSE] Error parsing SSE payload:", err);
      }
    };

    es.onerror = () => {
      setStatus("disconnected");
      es.close();

      // Attempt reconnection after 3 seconds
      if (reconnectTimeoutRef.current) clearTimeout(reconnectTimeoutRef.current);
      reconnectTimeoutRef.current = setTimeout(() => {
        connect();
      }, 3000);
    };
  }, []);

  useEffect(() => {
    connect();

    return () => {
      if (eventSourceRef.current) {
        eventSourceRef.current.close();
      }
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current);
      }
    };
  }, [connect]);

  return { status, lastEvent };
}
