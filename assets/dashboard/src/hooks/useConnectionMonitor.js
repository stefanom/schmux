import { useEffect, useState } from 'react';

const CHECK_INTERVAL = 5000;

export default function useConnectionMonitor() {
  const [connected, setConnected] = useState(true);

  useEffect(() => {
    let active = true;
    let intervalId;

    const check = async () => {
      try {
        const response = await fetch('/api/healthz');
        if (!active) return;
        setConnected(response.ok);
      } catch {
        if (!active) return;
        setConnected(false);
      }
    };

    check();
    intervalId = setInterval(check, CHECK_INTERVAL);

    return () => {
      active = false;
      if (intervalId) clearInterval(intervalId);
    };
  }, []);

  return connected;
}
