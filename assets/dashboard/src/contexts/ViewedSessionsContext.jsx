import React, { createContext, useContext, useCallback } from 'react';
import { useConfig } from './ConfigContext.jsx';
import useLocalStorage from '../hooks/useLocalStorage.js';

const ViewedSessionsContext = createContext();

export function ViewedSessionsProvider({ children }) {
  const [viewedSessions, setViewedSessions] = useLocalStorage('viewedSessions', {});
  const { config } = useConfig();

  const markAsViewed = useCallback((sessionId) => {
    // Add buffer to avoid spurious "New" badges from xterm.js control sequences on focus
    const buffer = config.internal?.viewed_buffer_ms || 5000;
    setViewedSessions((prev) => ({ ...prev, [sessionId]: Date.now() + buffer }));
  }, [config.internal?.viewed_buffer_ms, setViewedSessions]);

  return (
    <ViewedSessionsContext.Provider value={{ viewedSessions, markAsViewed }}>
      {children}
    </ViewedSessionsContext.Provider>
  );
}

export function useViewedSessions() {
  return useContext(ViewedSessionsContext);
}

