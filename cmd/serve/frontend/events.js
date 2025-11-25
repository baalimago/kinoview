(function() {
    function setupEventStream() {
        let wsProto = "ws://";
        if (window.location.protocol === "https:") {
            wsProto = "wss://";
        }
        const wsUrl = wsProto + window.location.host + "/gallery/ws";
        
        // Configuration
        const RECONNECT_DELAY = 1000;
        const UPDATE_INTERVAL = 5 * 60 * 1000; // 5 minutes

        let socket;
        let reconnectTimer;
        let updateIntervalTimer;

        function connect() {
            if (socket) {
                // Ensure clean state if we are forcing a reconnect
                try { socket.close(); } catch(e){}
            }

            console.log("Connecting to EventStream at " + wsUrl);
            socket = new WebSocket(wsUrl);

            socket.onopen = function() {
                console.log("EventStream connected");
                sendContext();
                // Start periodic updates
                if (updateIntervalTimer) clearInterval(updateIntervalTimer);
                updateIntervalTimer = setInterval(sendContext, UPDATE_INTERVAL);
            };

            socket.onmessage = function(event) {
                try {
                    const msg = JSON.parse(event.data);
                    if (msg.type === "health") {
                         socket.send(JSON.stringify({
                            type: "health",
                            time: new Date().toISOString(),
                            payload: {}
                        }));
                    }
                } catch (e) {
                    console.error("Failed to parse incoming event:", e);
                }
            };

            socket.onclose = function(event) {
                console.log("EventStream closed. Reconnecting in " + RECONNECT_DELAY + "ms");
                cleanup();
                reconnectTimer = setTimeout(connect, RECONNECT_DELAY);
            };

            socket.onerror = function(error) {
                console.error("EventStream error:", error);
                socket.close(); // Ensure onclose is called
            };
        }

        function cleanup() {
            if (updateIntervalTimer) clearInterval(updateIntervalTimer);
        }

        function sendContext() {
            if (!socket || socket.readyState !== WebSocket.OPEN) return;
            
            try {
                // Dependencies from index.js
                if (typeof constuctClientContext !== "function") {
                    console.warn("constuctClientContext not found, skipping update");
                    return;
                }

                const ctx = constuctClientContext();
                
                // Enhance context with lastPlayedName if available
                if (typeof mostRecentID !== "undefined" && mostRecentID) {
                    if (typeof loadPersistedMediaItem === "function") {
                        const item = loadPersistedMediaItem(mostRecentID);
                        if (item && item.name) {
                            ctx.lastPlayedName = item.name;
                        }
                    }
                }

                socket.send(JSON.stringify({
                    type: "clientContext",
                    time: new Date().toISOString(),
                    payload: ctx
                }));
                console.log("Sent user context update");
            } catch (e) {
                console.error("Failed to send user context:", e);
            }
        }

        // Start connection
        connect();
        
        // Handle page visibility changes to possibly reconnect or pause?
        // For now, browser WebSocket handling is sufficient.
    }

    // Initialize
    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", setupEventStream);
    } else {
        setupEventStream();
    }
})();
