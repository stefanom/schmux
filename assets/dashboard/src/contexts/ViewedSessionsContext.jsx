import React, { createContext, useState, useContext } from 'react';
import { useConfig } from './ConfigContext.jsx';

const ViewedSessionsContext = createContext();

export function ViewedSessionsProvider({ children }) {
  const [viewedSessions, setViewedSessions] = useState({}); // sessionId -> timestamp
  const { config } = useConfig();

  const markAsViewed = (sessionId) => {
    // Add buffer to avoid spurious "New" badges from xterm.js control sequences on focus
    const buffer = config.internal?.viewed_buffer_ms || 5000;
    setViewedSessions((prev) => ({ ...prev, [sessionId]: Date.now() + buffer }));
  };

  return (
    <ViewedSessionsContext.Provider value={{ viewedSessions, markAsViewed }}>
      {children}
    </ViewedSessionsContext.Provider>
  );
}

export function useViewedSessions() {
  return useContext(ViewedSessionsContext);
}

