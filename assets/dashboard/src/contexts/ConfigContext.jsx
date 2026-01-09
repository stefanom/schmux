import React, { createContext, useState, useContext, useEffect } from 'react';
import { getConfig } from '../lib/api.js';

const ConfigContext = createContext();

const DEFAULT_CONFIG = {
  internal: {
    mtime_poll_interval_ms: 5000,
    sessions_poll_interval_ms: 5000,
    viewed_buffer_ms: 5000,
    session_seen_interval_ms: 2000,
  }
};

export function ConfigProvider({ children }) {
  const [config, setConfig] = useState(DEFAULT_CONFIG);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    let active = true;

    const loadConfig = async () => {
      try {
        const data = await getConfig();
        if (!active) return;
        setConfig(data);
        setError(null);
      } catch (err) {
        if (!active) return;
        console.error('Failed to load config:', err);
        setError(err.message);
        // Keep using defaults on error
      } finally {
        if (active) setLoading(false);
      }
    };

    loadConfig();

    return () => {
      active = false;
    };
  }, []);

  return (
    <ConfigContext.Provider value={{ config, loading, error }}>
      {children}
    </ConfigContext.Provider>
  );
}

export function useConfig() {
  return useContext(ConfigContext);
}
