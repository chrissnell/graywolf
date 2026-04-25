/**
 * GENERATED FILE — DO NOT EDIT.
 *
 * Regenerate with `npm run api:generate` (or `make api-client` from
 * the repo root). Source of truth: pkg/webapi/docs/gen/swagger.json.
 */

export interface paths {
    "/agw": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Get AGW config */
        get: operations["getAgw"];
        /** Update AGW config */
        put: operations["updateAgw"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/audio-devices": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List audio devices */
        get: operations["listAudioDevices"];
        put?: never;
        /** Create audio device */
        post: operations["createAudioDevice"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/audio-devices/available": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List available audio devices */
        get: operations["listAvailableAudioDevices"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/audio-devices/levels": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Get audio device levels */
        get: operations["getAudioDeviceLevels"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/audio-devices/scan-levels": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Scan audio device input levels */
        post: operations["scanAudioDeviceLevels"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/audio-devices/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Get audio device */
        get: operations["getAudioDevice"];
        /** Update audio device */
        put: operations["updateAudioDevice"];
        post?: never;
        /** Delete audio device */
        delete: operations["deleteAudioDevice"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/audio-devices/{id}/gain": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        /** Set audio device gain */
        put: operations["setAudioDeviceGain"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/audio-devices/{id}/test-tone": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Play test tone on audio device */
        post: operations["playTestTone"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/auth/login": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Log in */
        post: operations["login"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/auth/logout": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Log out */
        post: operations["logout"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/auth/setup": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Get first-run setup status */
        get: operations["getSetupStatus"];
        put?: never;
        /** Create first-run user */
        post: operations["createFirstUser"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/beacons": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List beacons */
        get: operations["listBeacons"];
        put?: never;
        /** Create beacon */
        post: operations["createBeacon"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/beacons/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Get beacon */
        get: operations["getBeacon"];
        /** Update beacon */
        put: operations["updateBeacon"];
        post?: never;
        /** Delete beacon */
        delete: operations["deleteBeacon"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/beacons/{id}/send": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Send beacon now */
        post: operations["sendBeacon"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/channels": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List channels */
        get: operations["listChannels"];
        put?: never;
        /** Create channel */
        post: operations["createChannel"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/channels/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Get channel */
        get: operations["getChannel"];
        /** Update channel */
        put: operations["updateChannel"];
        post?: never;
        /** Delete channel */
        delete: operations["deleteChannel"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/channels/{id}/referrers": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List referrers of a channel */
        get: operations["getChannelReferrers"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/channels/{id}/stats": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Get channel stats */
        get: operations["getChannelStats"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/digipeater": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Get digipeater config */
        get: operations["getDigipeaterConfig"];
        /** Update digipeater config */
        put: operations["updateDigipeaterConfig"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/digipeater/rules": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List digipeater rules */
        get: operations["listDigipeaterRules"];
        put?: never;
        /** Create digipeater rule */
        post: operations["createDigipeaterRule"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/digipeater/rules/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        /** Update digipeater rule */
        put: operations["updateDigipeaterRule"];
        post?: never;
        /** Delete digipeater rule */
        delete: operations["deleteDigipeaterRule"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/gps": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Get GPS config */
        get: operations["getGps"];
        /** Update GPS config */
        put: operations["updateGps"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/gps/available": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List available GPS serial ports */
        get: operations["listAvailableGps"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/health": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Health check */
        get: operations["getHealth"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/igate": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Get igate status */
        get: operations["getIgateStatus"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/igate/config": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Get igate config */
        get: operations["getIgateConfig"];
        /** Update igate config */
        put: operations["updateIgateConfig"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/igate/filters": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List igate RF filters */
        get: operations["listIgateFilters"];
        put?: never;
        /** Create igate RF filter */
        post: operations["createIgateFilter"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/igate/filters/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        /** Update igate RF filter */
        put: operations["updateIgateFilter"];
        post?: never;
        /** Delete igate RF filter */
        delete: operations["deleteIgateFilter"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/igate/simulation": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Toggle igate simulation mode
         * @description When enabled, the igate logs packets it would have sent
         *     (RF-to-APRS-IS gating and IS-to-RF beacons) instead of
         *     transmitting them. Useful for validating filter rules
         *     and bandwidth behaviour before going live.
         */
        post: operations["setIgateSimulation"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/kiss": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List KISS interfaces */
        get: operations["listKiss"];
        put?: never;
        /** Create KISS interface */
        post: operations["createKiss"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/kiss/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Get KISS interface */
        get: operations["getKiss"];
        /** Update KISS interface */
        put: operations["updateKiss"];
        post?: never;
        /** Delete KISS interface */
        delete: operations["deleteKiss"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/kiss/{id}/reconnect": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Reconnect a KISS tcp-client interface now */
        post: operations["reconnectKiss"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/messages": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List messages */
        get: operations["listMessages"];
        put?: never;
        /** Send message */
        post: operations["sendMessage"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/messages/conversations": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List conversations */
        get: operations["listConversations"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/messages/events": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Stream message events */
        get: operations["streamMessageEvents"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/messages/preferences": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Get message preferences */
        get: operations["getMessagePreferences"];
        /** Update message preferences */
        put: operations["putMessagePreferences"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/messages/tactical": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List tactical callsigns */
        get: operations["listTacticalCallsigns"];
        put?: never;
        /** Create tactical callsign */
        post: operations["createTacticalCallsign"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/messages/tactical/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        /** Update tactical callsign */
        put: operations["updateTacticalCallsign"];
        post?: never;
        /** Delete tactical callsign */
        delete: operations["deleteTacticalCallsign"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/messages/tactical/{key}/participants": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Tactical thread participants */
        get: operations["getTacticalParticipants"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/messages/threads/{kind}/{key}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        post?: never;
        /** Delete message thread */
        delete: operations["deleteMessageThread"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/messages/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Get message */
        get: operations["getMessage"];
        put?: never;
        post?: never;
        /** Delete message */
        delete: operations["deleteMessage"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/messages/{id}/read": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Mark message read */
        post: operations["markMessageRead"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/messages/{id}/resend": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Resend message */
        post: operations["resendMessage"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/messages/{id}/unread": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Mark message unread */
        post: operations["markMessageUnread"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/packets": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List packets */
        get: operations["listPackets"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/position": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Get station position */
        get: operations["getPosition"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/position-log": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Get position log config */
        get: operations["getPositionLog"];
        /** Update position log config */
        put: operations["updatePositionLog"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/preferences/maps": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Get maps preference */
        get: operations["getMapsConfig"];
        /** Update maps preference */
        put: operations["updateMapsConfig"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/preferences/maps/register": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Register this device with auth.nw5w.com */
        post: operations["registerMapsToken"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/preferences/theme": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Get theme preference */
        get: operations["getThemeConfig"];
        /** Update theme preference */
        put: operations["updateThemeConfig"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/preferences/units": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Get units preference */
        get: operations["getUnitsConfig"];
        /** Update units preference */
        put: operations["updateUnitsConfig"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/ptt": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List PTT configs */
        get: operations["listPttConfigs"];
        put?: never;
        /** Upsert PTT config */
        post: operations["upsertPttConfig"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/ptt/available": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List PTT devices */
        get: operations["listPttDevices"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/ptt/capabilities": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Get PTT capabilities */
        get: operations["getPttCapabilities"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/ptt/gpio-chips/{chip}/lines": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List GPIO lines */
        get: operations["listGpioLines"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/ptt/test-rigctld": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Test rigctld connection */
        post: operations["testRigctld"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/ptt/{channel}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Get PTT config */
        get: operations["getPttConfig"];
        /** Update PTT config */
        put: operations["updatePttConfig"];
        post?: never;
        /** Delete PTT config */
        delete: operations["deletePttConfig"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/release-notes": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List all release notes */
        get: operations["listReleaseNotes"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/release-notes/ack": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Acknowledge every release note through the current build */
        post: operations["ackReleaseNotes"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/release-notes/unseen": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List unseen release notes for the caller */
        get: operations["listUnseenReleaseNotes"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/smart-beacon": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Get SmartBeacon configuration
         * @description Returns the global SmartBeacon parameters that control
         *     transmit cadence for every beacon with smart_beacon=true.
         *     Returns defaults when no configuration has been saved yet.
         */
        get: operations["getSmartBeacon"];
        /**
         * Update SmartBeacon configuration
         * @description Replaces the global SmartBeacon curve parameters. On
         *     success, re-reads the persisted row and returns it so
         *     the client sees the stored shape.
         */
        put: operations["updateSmartBeacon"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/station/config": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Get station config */
        get: operations["getStationConfig"];
        /** Update station config */
        put: operations["updateStationConfig"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/stations": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List stations */
        get: operations["listStations"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/stations/autocomplete": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Station autocomplete */
        get: operations["autocompleteStations"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/status": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** System status dashboard */
        get: operations["getStatus"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/tacticals": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Accept a tactical invite (subscribe) */
        post: operations["acceptTacticalInvite"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/tx-timing": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List tx-timing records */
        get: operations["listTxTiming"];
        put?: never;
        /** Upsert tx-timing record */
        post: operations["createTxTiming"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/tx-timing/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Get tx-timing record */
        get: operations["getTxTiming"];
        /** Update tx-timing record */
        put: operations["updateTxTiming"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/updates/config": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Get updates check configuration */
        get: operations["getUpdatesConfig"];
        /** Update updates check configuration */
        put: operations["updateUpdatesConfig"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/updates/status": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Get latest-release status */
        get: operations["getUpdatesStatus"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/version": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Get server version */
        get: operations["getVersion"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
}
export type webhooks = Record<string, never>;
export interface components {
    schemas: {
        "aprs.Capabilities": {
            /** @description key → value (value empty for flag entries) */
            entries?: {
                [key: string]: string;
            };
        };
        "aprs.DecodedAPRSPacket": {
            caps?: components["schemas"]["aprs.Capabilities"];
            channel?: number;
            /** @description residual free-form text after structured fields */
            comment?: string;
            dest?: string;
            df?: components["schemas"]["aprs.DirectionFinding"];
            /**
             * @description Direction identifies the ingress path: DirectionRF for packets heard
             *     over RF via the modem bridge / KISS / AGW, DirectionIS for packets
             *     received from APRS-IS by the iGate. Unset (DirectionUnknown) when
             *     the packet is synthesized (e.g. inner third-party decode, tests) or
             *     constructed before ingress provenance is known.
             */
            direction?: components["schemas"]["aprs.Direction"];
            item?: components["schemas"]["aprs.Item"];
            message?: components["schemas"]["aprs.Message"];
            micE?: components["schemas"]["aprs.MicE"];
            object?: components["schemas"]["aprs.Object"];
            path?: string[];
            position?: components["schemas"]["aprs.Position"];
            /** @description modem-reported quality (0..100) if available */
            quality?: number;
            /** @description original AX.25 frame bytes */
            raw?: number[];
            /** @description callsign-SSID */
            source?: string;
            /** @description for '>' status reports */
            status?: string;
            telemetry?: components["schemas"]["aprs.Telemetry"];
            /** @description PARM/UNIT/EQNS/BITS metadata (APRS101 ch 13) */
            telemetryMeta?: components["schemas"]["aprs.TelemetryMeta"];
            /** @description recursively-decoded inner packet for '}' traffic (APRS101 ch 20) */
            thirdParty?: components["schemas"]["aprs.DecodedAPRSPacket"];
            timestamp?: string;
            type?: components["schemas"]["aprs.PacketType"];
            weather?: components["schemas"]["aprs.Weather"];
        };
        "aprs.DeviceInfo": {
            class?: string;
            model?: string;
            vendor?: string;
        };
        /** @enum {string} */
        "aprs.Direction": "" | "rf" | "is";
        "aprs.DirectionFinding": {
            /** @description degrees true */
            bearing?: number;
            /** @description 0..9 station count */
            number?: number;
            /** @description 0..9 */
            quality?: number;
            /** @description miles */
            range?: number;
        };
        "aprs.Item": {
            comment?: string;
            live?: boolean;
            name?: string;
            position?: components["schemas"]["aprs.Position"];
        };
        "aprs.Message": {
            /** @description 1..9 chars, space-padded in packet */
            addressee?: string;
            /** @description true if a reply-ack trailer was present (ack may still be "") */
            hasReplyAck?: boolean;
            isAck?: boolean;
            /** @description addressee starts with BLN */
            isBulletin?: boolean;
            /** @description NWS-originated */
            isNWS?: boolean;
            isRej?: boolean;
            /** @description optional identifier used for ACK/REJ correlation */
            messageID?: string;
            /** @description piggybacked reply-ack id (aprs11/replyacks), empty if absent */
            replyAck?: string;
            text?: string;
        };
        "aprs.MicE": {
            /** @description e.g. "Kenwood TH-D74", "" if unknown */
            manufacturer?: string;
            /** @description 0..7 index into the standard Mic-E message table */
            messageCode?: number;
            messageText?: string;
            position?: components["schemas"]["aprs.Position"];
            /** @description trailing status text */
            status?: string;
        };
        "aprs.Object": {
            comment?: string;
            live?: boolean;
            name?: string;
            position?: components["schemas"]["aprs.Position"];
            timestamp?: string;
        };
        "aprs.PHG": {
            /** @description 0=omni, 1..8 = 45°·d compass direction (N, NE, E, …) */
            directivity?: number;
            /** @description g dB (0..9) */
            gainDB?: number;
            /** @description 10·2^h feet above average terrain */
            heightFt?: number;
            /** @description p² watts */
            powerWatts?: number;
            /** @description four-digit "phgd" body (e.g. "7700"); never includes the "PHG" prefix */
            raw?: string;
        };
        /** @enum {string} */
        "aprs.PacketType": "unknown" | "position" | "message" | "telemetry" | "weather" | "object" | "item" | "mic-e" | "status" | "capabilities" | "df-report" | "query" | "third-party";
        "aprs.Position": {
            /**
             * Format: float64
             * @description meters (0 if none reported)
             */
            altitude?: number;
            /** @description 0..4, digits of ambiguity introduced by spaces */
            ambiguity?: number;
            compressed?: boolean;
            /** @description degrees true (0..359) */
            course?: number;
            /**
             * Format: int32
             * @description DAO datum byte (APRS101 DAO extension), 0 if not present
             */
            daodatum?: number;
            hasAlt?: boolean;
            hasCourse?: boolean;
            /**
             * Format: float64
             * @description decimal degrees, positive north
             */
            latitude?: number;
            /** @description true if the timestamp was the '/' local-time form (APRS101 ch 6) */
            localTime?: boolean;
            /**
             * Format: float64
             * @description decimal degrees, positive east
             */
            longitude?: number;
            /** @description decoded Power/Height/Gain/Directivity extension (APRS101 ch 7), nil if not present */
            phg?: components["schemas"]["aprs.PHG"];
            /**
             * Format: float64
             * @description knots
             */
            speed?: number;
            symbol?: components["schemas"]["aprs.Symbol"];
            /** @description nil if positionless or no embedded time */
            timestamp?: string;
        };
        "aprs.Symbol": {
            /** Format: int32 */
            code?: number;
            /** Format: int32 */
            table?: number;
        };
        "aprs.Telemetry": {
            analog?: number[];
            /** @description true for channels actually reported (distinguishes 0 from missing) */
            analogHas?: boolean[];
            /** @description trailing free-form */
            comment?: string;
            /**
             * Format: int32
             * @description bits 0..7 (only lower 8)
             */
            digital?: number;
            hasDigital?: boolean;
            /** @description 0..999, -1 if absent */
            seq?: number;
        };
        "aprs.TelemetryMeta": {
            /**
             * Format: int32
             * @description BITS. sense-bits bitmap (active-high per bit)
             */
            bits?: number;
            /** @description a, b, c coefficients per analog channel */
            eqns?: number[][];
            /** @description "parm", "unit", "eqns", or "bits" */
            kind?: string;
            parm?: string[];
            /** @description BITS. project title */
            projectName?: string;
            unit?: string[];
        };
        "aprs.Weather": {
            hasHumidity?: boolean;
            hasLuminosity?: boolean;
            hasPressure?: boolean;
            hasRain1h?: boolean;
            hasRain24h?: boolean;
            hasRainMid?: boolean;
            hasRawRain?: boolean;
            hasSnow?: boolean;
            hasTemp?: boolean;
            hasWindDir?: boolean;
            hasWindGust?: boolean;
            hasWindSpeed?: boolean;
            /** @description percent (0..100) */
            humidity?: number;
            /** @description watts/m^2 */
            luminosity?: number;
            /**
             * Format: float64
             * @description tenths of millibar (e.g. 10132 = 1013.2)
             */
            pressure?: number;
            /**
             * Format: float64
             * @description hundredths of an inch
             */
            rain1Hour?: number;
            /** Format: float64 */
            rain24Hour?: number;
            /** Format: float64 */
            rainSinceMid?: number;
            /** @description raw rain counter ('#' field) */
            rawRainCounter?: number;
            /**
             * Format: float64
             * @description inches (via 's' after 'g')
             */
            snowfall24h?: number;
            /** @description one-letter software code (e.g. 'w', 'x', 'd') */
            softwareType?: string;
            /**
             * Format: float64
             * @description degrees F
             */
            temperature?: number;
            /** @description 2..4 ASCII letters identifying the unit/model */
            weatherUnitTag?: string;
            /** @description degrees true */
            windDirection?: number;
            /**
             * Format: float64
             * @description mph (5-minute peak)
             */
            windGust?: number;
            /**
             * Format: float64
             * @description mph (1-minute sustained)
             */
            windSpeed?: number;
        };
        "configstore.Referrer": {
            id?: number;
            name?: string;
            type?: string;
        };
        "dto.AcceptInviteRequest": {
            /**
             * @description Callsign is the tactical label to subscribe to. Required. Must
             *     match the APRS tactical syntax (1-9 of [A-Z0-9-]) after uppercase
             *     normalization.
             */
            callsign: string;
            /**
             * @description SourceMessageID, when non-zero, identifies the inbound invite
             *     message that triggered the accept. Used only for audit — the
             *     handler sets InviteAcceptedAt on that row if it resolves to a
             *     valid invite for the same tactical. Zero = accept without audit.
             */
            source_message_id?: number;
        };
        "dto.AcceptInviteResponse": {
            /**
             * @description AlreadyMember is true when the operator was already subscribed
             *     and enabled before this request. Lets the UI suppress the
             *     "Joined TAC" toast and emit "Already a member" instead.
             */
            already_member?: boolean;
            /**
             * @description Tactical is the post-accept state of the subscription. Always
             *     populated with Enabled=true (accept is the "turn it on" verb).
             */
            tactical?: components["schemas"]["dto.TacticalCallsignResponse"];
        };
        "dto.AgwRequest": {
            callsigns?: string;
            enabled?: boolean;
            listen_addr?: string;
        };
        "dto.AgwResponse": {
            callsigns?: string;
            enabled?: boolean;
            id?: number;
            listen_addr?: string;
        };
        "dto.AudioDeviceDeleteConflict": {
            channels?: components["schemas"]["dto.ChannelResponse"][];
            error?: string;
        };
        "dto.AudioDeviceDeleteResponse": {
            deleted?: components["schemas"]["dto.ChannelResponse"][];
        };
        "dto.AudioDeviceLevelsResponse": {
            [key: string]: components["schemas"]["modembridge.DeviceLevel"];
        };
        "dto.AudioDeviceRequest": {
            channels?: number;
            device_path?: string;
            direction?: string;
            format?: string;
            gain_db?: number;
            name?: string;
            sample_rate?: number;
            source_type?: string;
        };
        "dto.AudioDeviceResponse": {
            channels?: number;
            device_path?: string;
            direction?: string;
            format?: string;
            gain_db?: number;
            id?: number;
            name?: string;
            sample_rate?: number;
            source_type?: string;
        };
        "dto.AudioDeviceSetGainRequest": {
            gain_db?: number;
        };
        "dto.BeaconRequest": {
            alt_ft?: number;
            ambiguity?: number;
            callsign?: string;
            channel?: number;
            comment?: string;
            comment_cmd?: string;
            compress?: boolean;
            custom_info?: string;
            delay_seconds?: number;
            destination?: string;
            dir?: number;
            enabled?: boolean;
            freq?: string;
            freq_offset?: string;
            gain?: number;
            height?: number;
            interval?: number;
            latitude?: number;
            longitude?: number;
            messaging?: boolean;
            object_name?: string;
            overlay?: string;
            path?: string;
            power?: number;
            sb_fast_rate?: number;
            sb_fast_speed?: number;
            sb_min_turn_time?: number;
            sb_slow_rate?: number;
            sb_slow_speed?: number;
            sb_turn_angle?: number;
            sb_turn_slope?: number;
            send_to_aprs_is?: boolean;
            slot_seconds?: number;
            smart_beacon?: boolean;
            symbol?: string;
            symbol_table?: string;
            tone?: string;
            type?: string;
            use_gps?: boolean;
        };
        "dto.BeaconResponse": {
            alt_ft?: number;
            ambiguity?: number;
            callsign?: string;
            channel?: number;
            comment?: string;
            comment_cmd?: string;
            compress?: boolean;
            custom_info?: string;
            delay_seconds?: number;
            destination?: string;
            dir?: number;
            enabled?: boolean;
            freq?: string;
            freq_offset?: string;
            gain?: number;
            height?: number;
            id?: number;
            interval?: number;
            latitude?: number;
            longitude?: number;
            messaging?: boolean;
            object_name?: string;
            overlay?: string;
            path?: string;
            power?: number;
            sb_fast_rate?: number;
            sb_fast_speed?: number;
            sb_min_turn_time?: number;
            sb_slow_rate?: number;
            sb_slow_speed?: number;
            sb_turn_angle?: number;
            sb_turn_slope?: number;
            send_to_aprs_is?: boolean;
            slot_seconds?: number;
            smart_beacon?: boolean;
            symbol?: string;
            symbol_table?: string;
            tone?: string;
            type?: string;
            use_gps?: boolean;
        };
        "dto.BeaconSendResponse": {
            /** @description "sent" */
            status?: string;
        };
        "dto.ChannelBacking": {
            health?: string;
            kiss_tnc?: components["schemas"]["dto.ChannelKissTncEntry"][];
            modem?: components["schemas"]["dto.ChannelModemBacking"];
            summary?: string;
            tx?: components["schemas"]["dto.TxCapability"];
        };
        "dto.ChannelKissTncEntry": {
            allow_tx_from_governor?: boolean;
            interface_id?: number;
            interface_name?: string;
            last_error?: string;
            retry_at_unix_ms?: number;
            state?: string;
        };
        "dto.ChannelModemBacking": {
            active?: boolean;
            reason?: string;
        };
        "dto.ChannelRequest": {
            bit_rate?: number;
            decoder_offset?: number;
            fix_bits?: string;
            fx25_encode?: boolean;
            il2p_encode?: boolean;
            input_channel?: number;
            input_device_id?: number;
            mark_freq?: number;
            modem_type?: string;
            name?: string;
            num_decoders?: number;
            num_slicers?: number;
            output_channel?: number;
            output_device_id?: number;
            profile?: string;
            space_freq?: number;
        };
        "dto.ChannelResponse": {
            backing?: components["schemas"]["dto.ChannelBacking"];
            bit_rate?: number;
            decoder_offset?: number;
            fix_bits?: string;
            fx25_encode?: boolean;
            id?: number;
            il2p_encode?: boolean;
            input_channel?: number;
            input_device_id?: number;
            mark_freq?: number;
            modem_type?: string;
            name?: string;
            num_decoders?: number;
            num_slicers?: number;
            output_channel?: number;
            output_device_id?: number;
            profile?: string;
            space_freq?: number;
        };
        "dto.ConversationSummary": {
            alias?: string;
            archived?: boolean;
            last_at?: string;
            last_sender_call?: string;
            last_snippet?: string;
            participant_count?: number;
            thread_key?: string;
            thread_kind?: string;
            total_count?: number;
            unread_count?: number;
        };
        "dto.DigipeaterConfigRequest": {
            dedupe_window_seconds?: number;
            enabled?: boolean;
            my_call?: string;
        };
        "dto.DigipeaterConfigResponse": {
            dedupe_window_seconds?: number;
            enabled?: boolean;
            id?: number;
            my_call?: string;
        };
        "dto.DigipeaterRuleRequest": {
            action?: string;
            alias?: string;
            alias_type?: string;
            enabled?: boolean;
            from_channel?: number;
            max_hops?: number;
            priority?: number;
            to_channel?: number;
        };
        "dto.DigipeaterRuleResponse": {
            action?: string;
            alias?: string;
            alias_type?: string;
            enabled?: boolean;
            from_channel?: number;
            id?: number;
            max_hops?: number;
            priority?: number;
            to_channel?: number;
        };
        "dto.GPSRequest": {
            baud_rate?: number;
            gpsd_host?: string;
            gpsd_port?: number;
            serial_port?: string;
            source?: string;
        };
        "dto.GPSResponse": {
            baud_rate?: number;
            enabled?: boolean;
            gpsd_host?: string;
            gpsd_port?: number;
            id?: number;
            serial_port?: string;
            source?: string;
        };
        "dto.HealthResponse": {
            /** @description process start time, RFC3339 */
            started_at?: string;
            /** @description "ok" */
            status?: string;
            /** @description current UTC time, RFC3339 */
            time?: string;
        };
        "dto.IGateConfigRequest": {
            enabled?: boolean;
            gate_is_to_rf?: boolean;
            gate_rf_to_is?: boolean;
            max_msg_hops?: number;
            port?: number;
            rf_channel?: number;
            server?: string;
            server_filter?: string;
            simulation_mode?: boolean;
            software_name?: string;
            software_version?: string;
            tx_channel?: number;
        };
        "dto.IGateConfigResponse": {
            enabled?: boolean;
            gate_is_to_rf?: boolean;
            gate_rf_to_is?: boolean;
            id?: number;
            max_msg_hops?: number;
            port?: number;
            rf_channel?: number;
            server?: string;
            server_filter?: string;
            simulation_mode?: boolean;
            software_name?: string;
            software_version?: string;
            tx_channel?: number;
        };
        "dto.IGateRfFilterRequest": {
            action?: string;
            channel?: number;
            enabled?: boolean;
            pattern?: string;
            priority?: number;
            type?: string;
        };
        "dto.IGateRfFilterResponse": {
            action?: string;
            channel?: number;
            enabled?: boolean;
            id?: number;
            pattern?: string;
            priority?: number;
            type?: string;
        };
        "dto.KissRequest": {
            /**
             * @description AllowTxFromGovernor opts this TNC-mode interface in to receive
             *     frames from the TX governor (beacon / digipeater / iGate /
             *     KISS / AGW submissions). Only meaningful when Mode == "tnc";
             *     the validator rejects true with any other mode. Default false
             *     on migrated rows so existing TNC servers do not silently start
             *     transmitting; Phase 4 sets the DTO default to true for newly
             *     created tcp-client rows.
             */
            allow_tx_from_governor?: boolean;
            baud_rate?: number;
            channel?: number;
            mode?: string;
            reconnect_init_ms?: number;
            reconnect_max_ms?: number;
            /**
             * @description Tcp-client fields (Phase 4): RemoteHost:RemotePort is the dial
             *     target; ReconnectInitMs / ReconnectMaxMs size the supervisor's
             *     exponential-backoff reconnect schedule. Unused / zero for
             *     Type != "tcp-client".
             */
            remote_host?: string;
            remote_port?: number;
            serial_device?: string;
            tcp_port?: number;
            tnc_ingress_burst?: number;
            tnc_ingress_rate_hz?: number;
            type?: string;
        };
        "dto.KissResponse": {
            allow_tx_from_governor?: boolean;
            backoff_seconds?: number;
            baud_rate?: number;
            channel?: number;
            connected_since?: number;
            id?: number;
            last_error?: string;
            mode?: string;
            needs_reconfig?: boolean;
            peer_addr?: string;
            reconnect_count?: number;
            reconnect_init_ms?: number;
            reconnect_max_ms?: number;
            /** @description Tcp-client fields (Phase 4). Zero-valued for non-tcp-client rows. */
            remote_host?: string;
            remote_port?: number;
            retry_at_unix_ms?: number;
            serial_device?: string;
            /**
             * @description Per-interface runtime status (Phase 4). Surfaced verbatim from
             *     kiss.Manager.Status(); zero-valued when the row is not running
             *     or when the manager has nothing to report. Omitted from the
             *     wire when the interface is not tcp-client (State == "" +
             *     omitempty).
             */
            state?: string;
            tcp_port?: number;
            tnc_ingress_burst?: number;
            tnc_ingress_rate_hz?: number;
            type?: string;
        };
        "dto.MapsConfigRequest": {
            source?: string;
        };
        "dto.MapsConfigResponse": {
            callsign?: string;
            registered?: boolean;
            registered_at?: string;
            source?: string;
            token?: string;
        };
        "dto.MessageChange": {
            id?: number;
            kind?: string;
            message?: components["schemas"]["dto.MessageResponse"];
        };
        "dto.MessageListResponse": {
            changes?: components["schemas"]["dto.MessageChange"][];
            cursor?: string;
        };
        "dto.MessagePreferencesRequest": {
            default_path?: string;
            fallback_policy?: string;
            /**
             * @description MaxMessageTextOverride raises the default 67-char addressee-line
             *     direct-message cap. 0 (or field absent) means "use the default";
             *     any positive value must fall in [MaxMessageText+1, MaxMessageTextUnsafe]
             *     (68..200). Applies to addressee-line DMs only — bulletins, status
             *     beacons, and position/weather frames are unaffected. The server
             *     rejects out-of-range values with 400 rather than silently clamping
             *     so operators see a clear error.
             */
            max_message_text_override?: number;
            retention_days?: number;
            retry_max_attempts?: number;
        };
        "dto.MessagePreferencesResponse": {
            default_path?: string;
            fallback_policy?: string;
            /**
             * @description MaxMessageTextOverride mirrors the request field on read. 0
             *     means "default enforce 67" — older servers that have never been
             *     upgraded return 0 here, which is also what a fresh singleton with
             *     no override set returns. Positive values fall in
             *     (MaxMessageText, MaxMessageTextUnsafe].
             */
            max_message_text_override?: number;
            retention_days?: number;
            retry_max_attempts?: number;
        };
        "dto.MessageResponse": {
            acked_at?: string;
            attempts?: number;
            channel?: number;
            created_at?: string;
            /** @description "in" | "out" */
            direction?: string;
            /**
             * @description Extended is true when the transmitted body exceeded the default
             *     MaxMessageText (67). The UI renders an "extended" badge on these
             *     rows so operators can correlate if recipients report missing or
             *     truncated messages. Derived from len(Text) > MaxMessageText; no
             *     dedicated column.
             */
            extended?: boolean;
            failure_reason?: string;
            from_call?: string;
            id?: number;
            /**
             * @description InviteAcceptedAt is audit-only: set when the local operator
             *     accepted this invite. The UI must NOT use this to decide "joined"
             *     state — that comes from the live TacticalSet cache. Kept so
             *     operators can see when/if an invite was acted on.
             */
            invite_accepted_at?: string;
            /**
             * @description InviteTactical is the tactical callsign referenced by an invite.
             *     Empty (and omitted) on non-invite rows.
             */
            invite_tactical?: string;
            is_ack?: boolean;
            is_bulletin?: boolean;
            /**
             * @description Kind is the body classification. Always populated — "text" for
             *     normal messages, "invite" for tactical invitations. Never omitted
             *     so clients can use a simple equality check without worrying about
             *     a legacy empty string.
             */
            kind?: string;
            msg_id?: string;
            next_retry_at?: string;
            our_call?: string;
            path?: string;
            peer_call?: string;
            received_at?: string;
            received_by_call?: string;
            sent_at?: string;
            /** @description "rf" | "is" */
            source?: string;
            /** @description derived — see DeriveMessageStatus */
            status?: string;
            text?: string;
            thread_key?: string;
            thread_kind?: string;
            to_call?: string;
            unread?: boolean;
            via?: string;
        };
        "dto.ParticipantResponse": {
            callsign?: string;
            last_active?: string;
            message_count?: number;
        };
        "dto.ParticipantsEnvelope": {
            effective_within_days?: number;
            participants?: components["schemas"]["dto.ParticipantResponse"][];
        };
        "dto.PositionLogRequest": {
            enabled?: boolean;
        };
        "dto.PositionLogResponse": {
            db_path?: string;
            enabled?: boolean;
        };
        "dto.PttRequest": {
            channel_id?: number;
            device_path?: string;
            dwait_ms?: number;
            /**
             * @description GpioLine is the gpiochip v2 line offset (0-indexed) used by the `gpio`
             *     method. Ignored for every other method.
             */
            gpio_line?: number;
            /**
             * @description GpioPin is the CM108 HID GPIO pin number (1-indexed, default 3). Not used
             *     by the `gpio` method, which references `gpio_line` instead to avoid
             *     indexing ambiguity between CM108 pin numbers and gpiochip line offsets.
             */
            gpio_pin?: number;
            invert?: boolean;
            method?: string;
            persist?: number;
            slot_time_ms?: number;
        };
        "dto.PttResponse": {
            channel_id?: number;
            device_path?: string;
            dwait_ms?: number;
            /**
             * @description GpioLine is the gpiochip v2 line offset (0-indexed) used by the `gpio`
             *     method. Ignored for every other method.
             */
            gpio_line?: number;
            /**
             * @description GpioPin is the CM108 HID GPIO pin number (1-indexed, default 3). Not used
             *     by the `gpio` method, which references `gpio_line` instead to avoid
             *     indexing ambiguity between CM108 pin numbers and gpiochip line offsets.
             */
            gpio_pin?: number;
            id?: number;
            invert?: boolean;
            method?: string;
            persist?: number;
            slot_time_ms?: number;
        };
        "dto.RegisterRequest": {
            callsign?: string;
        };
        "dto.RegisterResponse": {
            callsign?: string;
            registered?: boolean;
            registered_at?: string;
            source?: string;
            token?: string;
        };
        "dto.ReleaseNoteDTO": {
            /** @description pre-sanitized HTML */
            body?: string;
            /** @description ISO YYYY-MM-DD */
            date?: string;
            schema_version?: number;
            /** @description "info" | "cta" */
            style?: string;
            title?: string;
            version?: string;
        };
        "dto.ReleaseNotesResponse": {
            current?: string;
            /**
             * @description LastSeen is the authenticated caller's last acknowledged release
             *     version at the moment the request was served. Empty on the
             *     /api/release-notes endpoint (caller-agnostic) and on /unseen for
             *     a user who has never acked. The frontend uses this to render a
             *     "Since your last visit · vA → vB" subtitle in the news popup.
             */
            last_seen?: string;
            notes?: components["schemas"]["dto.ReleaseNoteDTO"][];
            schema_version?: number;
        };
        "dto.SendMessageRequest": {
            /** @description Channel overrides the configured TX channel. Nil = use default. */
            channel?: number;
            /**
             * @description ClientID is an opaque client-side correlation token. Echoed back
             *     unchanged in the response so the optimistic UI can reconcile its
             *     local row with the persisted ID.
             */
            client_id?: string;
            /**
             * @description InviteTactical is the tactical callsign referenced by an invite.
             *     Must be set when Kind == "invite"; ignored otherwise.
             */
            invite_tactical?: string;
            /**
             * @description Kind classifies the outbound row. Empty or "text" is a normal
             *     DM/tactical message; "invite" makes the sender build a
             *     `!GW1 INVITE <InviteTactical>` body and stamp the row with
             *     Kind=invite + InviteTactical. The sender (Phase 2) is
             *     responsible for honoring this; the DTO just carries it.
             */
            kind?: string;
            /**
             * @description Path overrides the default RF path from preferences. Empty =
             *     use MessagePreferences.DefaultPath.
             */
            path?: string;
            /**
             * @description PreferIS, when true, routes the outbound via APRS-IS regardless
             *     of the current fallback policy.
             */
            prefer_is?: boolean;
            /**
             * @description Text is the message body (<= 67 APRS chars after validation).
             *     Ignored when Kind == "invite" — the server builds the wire body
             *     from InviteTactical.
             */
            text?: string;
            /**
             * @description To is the addressee: a station callsign for a DM or a tactical
             *     label for a group broadcast. Uppercase-normalized server-side.
             */
            to?: string;
        };
        "dto.SmartBeaconConfigRequest": {
            /**
             * @description Enabled is true when SmartBeacon curve computation is active.
             *     When false, every beacon with smart_beacon=true falls back to
             *     its fixed interval.
             */
            enabled?: boolean;
            /**
             * @description FastRateSec is the beacon interval in seconds at or above
             *     FastSpeedKt. Must be shorter than SlowRateSec.
             */
            fast_rate?: number;
            /**
             * @description FastSpeedKt is the knots threshold at or above which beacons
             *     transmit at FastRateSec. The "moving fast" end of the curve.
             *     Must be greater than SlowSpeedKt.
             */
            fast_speed?: number;
            /**
             * @description MinTurnDeg is the fixed-component turn angle threshold, in
             *     degrees, used in the corner-pegging formula. Must be in
             *     [1, 179].
             */
            min_turn_angle?: number;
            /**
             * @description MinTurnSec is the minimum interval in seconds between
             *     turn-triggered beacons. Must be greater than zero.
             */
            min_turn_time?: number;
            /**
             * @description SlowRateSec is the beacon interval in seconds at or below
             *     SlowSpeedKt. Must be longer than FastRateSec.
             */
            slow_rate?: number;
            /**
             * @description SlowSpeedKt is the knots threshold at or below which beacons
             *     transmit at SlowRateSec. Must be greater than zero to prevent a
             *     degenerate middle-branch division by zero inside
             *     beacon.SmartBeaconConfig.Interval().
             */
            slow_speed?: number;
            /**
             * @description TurnSlope is the speed-dependent component (degrees·knots) of
             *     the corner-pegging turn threshold. Higher speed → lower
             *     effective threshold → corner pegs fire sooner. Must be greater
             *     than zero.
             */
            turn_slope?: number;
        };
        "dto.SmartBeaconConfigResponse": {
            /** @description Enabled is true when SmartBeacon curve computation is active. */
            enabled?: boolean;
            /**
             * @description FastRateSec is the beacon interval in seconds at or above
             *     FastSpeedKt.
             */
            fast_rate?: number;
            /**
             * @description FastSpeedKt is the knots threshold at or above which beacons
             *     transmit at FastRateSec.
             */
            fast_speed?: number;
            /**
             * @description MinTurnDeg is the fixed-component turn angle threshold in
             *     degrees.
             */
            min_turn_angle?: number;
            /**
             * @description MinTurnSec is the minimum interval in seconds between
             *     turn-triggered beacons.
             */
            min_turn_time?: number;
            /**
             * @description SlowRateSec is the beacon interval in seconds at or below
             *     SlowSpeedKt.
             */
            slow_rate?: number;
            /**
             * @description SlowSpeedKt is the knots threshold at or below which beacons
             *     transmit at SlowRateSec.
             */
            slow_speed?: number;
            /**
             * @description TurnSlope is the speed-dependent component (degrees·knots) of
             *     the corner-pegging turn threshold.
             */
            turn_slope?: number;
        };
        "dto.StationAutocomplete": {
            callsign?: string;
            description?: string;
            /** @description RFC3339, empty for bots / missing */
            last_heard?: string;
            /** @description "bot" | "cache" | "history" | "cache+history" */
            source?: string;
        };
        "dto.StationConfigRequest": {
            callsign?: string;
        };
        "dto.StationConfigResponse": {
            callsign?: string;
            disabled?: string[];
        };
        "dto.TacticalCallsignRequest": {
            alias?: string;
            callsign?: string;
            enabled?: boolean;
        };
        "dto.TacticalCallsignResponse": {
            alias?: string;
            callsign?: string;
            created_at?: string;
            enabled?: boolean;
            id?: number;
            updated_at?: string;
        };
        "dto.TestRigctldRequest": {
            host?: string;
            port?: number;
        };
        "dto.TestRigctldResponse": {
            latency_ms?: number;
            message?: string;
            ok?: boolean;
        };
        "dto.TestToneResponse": {
            status?: string;
        };
        "dto.ThemeConfigRequest": {
            id?: string;
        };
        "dto.ThemeConfigResponse": {
            id?: string;
        };
        "dto.TxCapability": {
            capable?: boolean;
            reason?: string;
        };
        "dto.TxTimingRequest": {
            channel?: number;
            full_dup?: boolean;
            persist?: number;
            rate_1min?: number;
            rate_5min?: number;
            slot_ms?: number;
            tx_delay_ms?: number;
            tx_tail_ms?: number;
        };
        "dto.TxTimingResponse": {
            channel?: number;
            full_dup?: boolean;
            id?: number;
            persist?: number;
            rate_1min?: number;
            rate_5min?: number;
            slot_ms?: number;
            tx_delay_ms?: number;
            tx_tail_ms?: number;
        };
        "dto.UnitsConfigRequest": {
            system?: string;
        };
        "dto.UnitsConfigResponse": {
            system?: string;
        };
        "dto.UpdatesConfigRequest": {
            enabled?: boolean;
        };
        "dto.UpdatesConfigResponse": {
            enabled?: boolean;
        };
        "dto.UpdatesStatusResponse": {
            /** @description RFC3339, omitted if zero */
            checked_at?: string;
            current?: string;
            latest?: string;
            status?: string;
            url?: string;
        };
        "gps.SerialPortInfo": {
            /** @description human-readable description */
            description?: string;
            is_usb?: boolean;
            /** @description basename of path */
            name?: string;
            /** @description device path, e.g. /dev/cu.usbserial-110 */
            path?: string;
            pid?: string;
            product?: string;
            /**
             * @description Recommended is true for the device path users should pick. On macOS
             *     we recommend the /dev/cu.* callout device over /dev/tty.* (which
             *     blocks until DCD is asserted).
             */
            recommended?: boolean;
            serial_number?: string;
            vid?: string;
            /**
             * @description Warning is set when there's a known gotcha with this path (e.g. the
             *     macOS tty.* / cu.* distinction).
             */
            warning?: string;
        };
        "igate.Status": {
            callsign?: string;
            connected?: boolean;
            is_to_rf_gated?: number;
            last_connected?: string;
            packets_filtered?: number;
            rf_to_is_dropped?: number;
            rf_to_is_gated?: number;
            server?: string;
            simulation_mode?: boolean;
        };
        "modembridge.AvailableDevice": {
            channels?: number[];
            /** @description human-friendly name (e.g. USB product string) */
            description?: string;
            host_api?: string;
            is_default?: boolean;
            is_input?: boolean;
            name?: string;
            /** @description pcm_id (used as device_path in config) */
            path?: string;
            /** @description true for plughw: devices (ALSA software conversion) */
            recommended?: boolean;
            sample_rates?: number[];
        };
        "modembridge.ChannelStats": {
            audio_level_mark?: number;
            audio_level_peak?: number;
            audio_level_space?: number;
            channel?: number;
            dcd_state?: boolean;
            dcd_transitions?: number;
            rx_bad_fcs?: number;
            rx_frames?: number;
            tx_frames?: number;
        };
        "modembridge.DeviceLevel": {
            clipping?: boolean;
            device_id?: number;
            peak_dbfs?: number;
            rms_dbfs?: number;
        };
        "modembridge.InputLevel": {
            error?: string;
            has_signal?: boolean;
            name?: string;
            peak_dbfs?: number;
        };
        /** @enum {string} */
        "packetlog.Direction": "RX" | "TX" | "IS";
        "pttdevice.AvailableDevice": {
            /** @description human-friendly label (USB product, GPIO chip) */
            description?: string;
            name?: string;
            path?: string;
            /**
             * @description Recommended is true for the device path users should prefer. On macOS
             *     we recommend /dev/cu.* over /dev/tty.* (which blocks until DCD).
             */
            recommended?: boolean;
            /** @description serial, gpio, cm108 */
            type?: string;
            usb_product?: string;
            usb_vendor?: string;
            /** @description Warning is set when there's a known gotcha with this path. */
            warning?: string;
        };
        "pttdevice.GpioLineInfo": {
            /** @description Consumer is the label of the driver currently holding the line, if any. */
            consumer?: string;
            /**
             * @description Name is the kernel-assigned line name. May be empty if the line is
             *     unnamed on this chip.
             */
            name?: string;
            /** @description Offset is the 0-indexed line offset within the chip. */
            offset?: number;
            /**
             * @description Used is true when another driver has claimed this line (e.g. SPI, I2C,
             *     UART, or a previously-running graywolf process).
             */
            used?: boolean;
        };
        "webapi.ChannelReferrersResponse": {
            error?: string;
            referrers?: components["schemas"]["configstore.Referrer"][];
        };
        "webapi.IgateSimulationResponse": {
            simulation_mode?: boolean;
        };
        "webapi.IgateToggleRequest": {
            enabled?: boolean;
        };
        "webapi.PositionDTO": {
            alt_m?: number;
            has_alt?: boolean;
            has_course?: boolean;
            heading_deg?: number;
            lat?: number;
            lon?: number;
            /** @description "gps", "fixed", or "none" */
            source?: string;
            speed_kt?: number;
            timestamp?: string;
            valid?: boolean;
        };
        "webapi.StationDTO": {
            /** @description Callsign is the station or object name (APRS callsign-SSID for stations, object/item name otherwise). */
            callsign?: string;
            /** @description Channel is the graywolf channel ID that received the most recent packet. */
            channel?: number;
            /** @description Comment is the free-form comment field from the most recent packet. */
            comment?: string;
            /** @description Direction indicates the source of the most recent packet: "RX" (heard on air), "TX" (sent by us), or "IS" (APRS-IS). */
            direction?: string;
            /** @description Hops is the APRS digipeater hop count (number of H-bit entries in Path). */
            hops?: number;
            /** @description IsObject is true for APRS objects and items; false for regular stations. */
            is_object?: boolean;
            /** @description LastHeard is the UTC RFC3339 timestamp of the most recent packet from this station. */
            last_heard?: string;
            /** @description Path is the raw AX.25 digipeater path from the most recent packet (entries with trailing "*" have the H-bit set). */
            path?: string[];
            /** @description PathPositions lists [lat, lon] pairs resolved for H-bit digipeaters in Path; zero pair when position is unknown. */
            path_positions?: number[][];
            /** @description Positions is the station's position history, newest first; static stations have exactly one entry. */
            positions?: components["schemas"]["webapi.StationPosDTO"][];
            /** @description SymbolCode is the APRS symbol code character within the selected table. */
            symbol_code?: string;
            /** @description SymbolTable is the APRS symbol table character ("/" primary, "\\" alternate, or an overlay char). */
            symbol_table?: string;
            /** @description Via is the callsign of the last digipeater in the most recent packet's H-bit path; empty for direct packets. */
            via?: string;
            /** @description Weather is optional weather telemetry; present only when include=weather is requested and the station reports weather. */
            weather?: components["schemas"]["webapi.WeatherDTO"];
        };
        "webapi.StationPosDTO": {
            /** @description Alt is the reported altitude in meters above mean sea level; omitted when not reported. */
            alt_m?: number;
            /** @description Channel is the graywolf channel ID that received this position packet. */
            channel?: number;
            /** @description Comment is the free-form comment field from this position packet. */
            comment?: string;
            /** @description Course is the reported course over ground in degrees true (0-359); omitted when not reported. */
            course?: number;
            /** @description Direction indicates the source of this position packet: "RX", "TX", or "IS". */
            direction?: string;
            /** @description HasAlt is true when the originating packet reported an altitude. */
            has_alt?: boolean;
            /** @description Hops is the APRS digipeater hop count (number of H-bit entries in Path) for this fix. */
            hops?: number;
            /** @description Lat is the reported latitude in decimal degrees (WGS84, north positive). */
            lat?: number;
            /** @description Lon is the reported longitude in decimal degrees (WGS84, east positive). */
            lon?: number;
            /** @description Path is the AX.25 digipeater path recorded with this position fix. */
            path?: string[];
            /** @description PathPositions lists [lat, lon] pairs resolved for H-bit digipeaters in Path; zero pair when position is unknown. */
            path_positions?: number[][];
            /** @description Speed is the reported ground speed in knots; omitted when not reported. */
            speed_kt?: number;
            /** @description Timestamp is the UTC RFC3339 time the position was received. */
            timestamp?: string;
            /** @description Via is the callsign of the last digipeater (H-bit) that forwarded this position packet; empty for direct. */
            via?: string;
        };
        "webapi.StatusChannel": {
            audio_peak?: number;
            bit_rate?: number;
            dcd_state?: boolean;
            device_clipping?: boolean;
            device_peak_dbfs?: number;
            device_rms_dbfs?: number;
            id?: number;
            input_device_id?: number;
            modem_type?: string;
            name?: string;
            rx_bad_fcs?: number;
            rx_frames?: number;
            tx_frames?: number;
        };
        "webapi.StatusDTO": {
            /** @description Channels is the per-channel live status snapshot (frame counters, audio levels). */
            channels?: components["schemas"]["webapi.StatusChannel"][];
            /** @description Igate is the current iGate session state; omitted when no iGate is configured. */
            igate?: components["schemas"]["webapi.StatusIgateDTO"];
            /** @description UptimeSeconds is the server process uptime in whole seconds. */
            uptime_seconds?: number;
        };
        "webapi.StatusIgateDTO": {
            /** @description Callsign is the login callsign-SSID presented to APRS-IS. */
            callsign?: string;
            /** @description Connected is true while an APRS-IS session is established. */
            connected?: boolean;
            /** @description Downlinked is the cumulative count of IS packets transmitted on RF. */
            is_to_rf_gated?: number;
            /** @description LastConnected is the UTC RFC3339 timestamp of the most recent successful IS login; omitted if never connected. */
            last_connected?: string;
            /** @description Filtered is the cumulative count of packets dropped by the filter engine. */
            packets_filtered?: number;
            /** @description DroppedOffline is the cumulative count of RF packets dropped because the IS session was offline. */
            rf_to_is_dropped?: number;
            /** @description Gated is the cumulative count of RF packets forwarded to APRS-IS. */
            rf_to_is_gated?: number;
            /** @description Server is the APRS-IS host:port currently in use (or last attempted). */
            server?: string;
            /** @description SimulationMode is true when RF->IS uploads are suppressed for testing. */
            simulation_mode?: boolean;
        };
        "webapi.VersionResponse": {
            version?: string;
        };
        "webapi.WeatherDTO": {
            /** @description WindGust is the peak wind gust in miles per hour; nil when not reported. */
            gust_mph?: number;
            /** @description Humidity is the relative humidity in percent (0-100); nil when not reported. */
            humidity?: number;
            /** @description Luminosity is solar radiation in watts per square meter; nil when not reported. */
            luminosity_wm2?: number;
            /** @description Pressure is the barometric pressure in millibars; nil when not reported. */
            pressure_mb?: number;
            /** @description Rain1h is rainfall in the last hour in inches; nil when not reported. */
            rain_1h_in?: number;
            /** @description Rain24h is rainfall in the last 24 hours in inches; nil when not reported. */
            rain_24h_in?: number;
            /** @description Snow24h is snowfall in the last 24 hours in inches; nil when not reported. */
            snow_24h_in?: number;
            /** @description Temperature is the ambient temperature in degrees Fahrenheit; nil when not reported. */
            temp_f?: number;
            /** @description WindDir is the wind direction in degrees true (0-359); nil when not reported. */
            wind_dir?: number;
            /** @description WindSpeed is the sustained wind speed in miles per hour; nil when not reported. */
            wind_mph?: number;
        };
        "webapi.packetDTO": {
            /** @description Channel is the graywolf channel ID that observed or transmitted the packet. */
            channel?: number;
            /** @description Decoded is the parsed APRS payload when decoding succeeded; nil otherwise. */
            decoded?: components["schemas"]["aprs.DecodedAPRSPacket"];
            /** @description Device is APRS device identification (manufacturer, model) inferred from the TOCALL field; omitted when unknown. */
            device?: components["schemas"]["aprs.DeviceInfo"];
            /** @description Direction labels the flow: "RX" (heard on air), "TX" (transmitted by us), or "IS" (APRS-IS upload/download). */
            direction?: components["schemas"]["packetlog.Direction"];
            /** @description Display is a direwolf-style human-readable rendering: "SRC>DEST[,DIGI*]:info". */
            display?: string;
            /** @description DistanceMi is the great-circle distance from this station's GPS fix to the packet's reported position, in statute miles; omitted when either position is unavailable. */
            distance_mi?: number;
            /** @description Notes is a short annotation describing how this entry was handled (e.g. "deduped", "rate-limited", "digi consumed WIDE1-1"). */
            notes?: string;
            /** @description Raw is the on-air AX.25 frame bytes with FCS stripped; omitted for entries without raw framing. */
            raw?: number[];
            /** @description Source identifies the subsystem that produced this entry: "kiss", "agw", "digi", "igate-tx", "beacon", "modem", or "igate-is". */
            source?: string;
            /** @description Timestamp is the UTC RFC3339 time the packet was recorded. */
            timestamp?: string;
            /** @description Type is the APRS packet type (position, message, status, ...) when the payload decoded successfully. */
            type?: string;
            /** @description Via is the callsign of the last digipeater that forwarded this packet (H-bit set); empty string for direct packets. */
            via?: string;
        };
        "webapi.pttCapabilities": {
            /**
             * @description PlatformSupportsGpio is true on Linux, where the gpiochip v2
             *     character-device API is available. The UI consults this flag —
             *     not the presence of enumerated chips — to decide whether the
             *     GPIO method appears in its dropdown, so a Linux host without
             *     any detected chips still shows GPIO with an explained empty
             *     state rather than silently omitting the option.
             */
            platform_supports_gpio?: boolean;
        };
        "webauth.LoginRequest": {
            password?: string;
            username?: string;
        };
        "webauth.SetupCreatedResponse": {
            status?: string;
            username?: string;
        };
        "webauth.SetupRequest": {
            password?: string;
            username?: string;
        };
        "webauth.SetupStatusResponse": {
            needs_setup?: boolean;
        };
        "webauth.StatusResponse": {
            status?: string;
        };
        "webtypes.ErrorResponse": {
            error?: string;
        };
    };
    responses: never;
    parameters: never;
    requestBodies: {
        /** @description Igate RF filter definition */
        "dto.IGateRfFilterRequest": {
            content: {
                "application/json": components["schemas"]["dto.IGateRfFilterRequest"];
            };
        };
        /** @description Tx-timing definition */
        "dto.TxTimingRequest": {
            content: {
                "application/json": components["schemas"]["dto.TxTimingRequest"];
            };
        };
        /** @description Audio device definition */
        "dto.AudioDeviceRequest": {
            content: {
                "application/json": components["schemas"]["dto.AudioDeviceRequest"];
            };
        };
        /** @description Beacon definition */
        "dto.BeaconRequest": {
            content: {
                "application/json": components["schemas"]["dto.BeaconRequest"];
            };
        };
        /** @description Channel definition */
        "dto.ChannelRequest": {
            content: {
                "application/json": components["schemas"]["dto.ChannelRequest"];
            };
        };
        /** @description Digipeater rule definition */
        "dto.DigipeaterRuleRequest": {
            content: {
                "application/json": components["schemas"]["dto.DigipeaterRuleRequest"];
            };
        };
        /** @description KISS interface definition */
        "dto.KissRequest": {
            content: {
                "application/json": components["schemas"]["dto.KissRequest"];
            };
        };
        /** @description Tactical definition */
        "dto.TacticalCallsignRequest": {
            content: {
                "application/json": components["schemas"]["dto.TacticalCallsignRequest"];
            };
        };
        /** @description PTT config */
        "dto.PttRequest": {
            content: {
                "application/json": components["schemas"]["dto.PttRequest"];
            };
        };
    };
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export interface operations {
    getAgw: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.AgwResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    updateAgw: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description AGW config */
        requestBody: {
            content: {
                "application/json": components["schemas"]["dto.AgwRequest"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.AgwResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    listAudioDevices: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.AudioDeviceResponse"][];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    createAudioDevice: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: components["requestBodies"]["dto.AudioDeviceRequest"];
        responses: {
            /** @description Created */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.AudioDeviceResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    listAvailableAudioDevices: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["modembridge.AvailableDevice"][];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    getAudioDeviceLevels: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.AudioDeviceLevelsResponse"];
                };
            };
        };
    };
    scanAudioDeviceLevels: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["modembridge.InputLevel"][];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    getAudioDevice: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description Audio device id */
                id: number;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.AudioDeviceResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Not Found */
            404: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    updateAudioDevice: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description Audio device id */
                id: number;
            };
            cookie?: never;
        };
        requestBody: components["requestBodies"]["dto.AudioDeviceRequest"];
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.AudioDeviceResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    deleteAudioDevice: {
        parameters: {
            query?: {
                /** @description Cascade-delete referencing channels */
                cascade?: boolean;
            };
            header?: never;
            path: {
                /** @description Audio device id */
                id: number;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.AudioDeviceDeleteResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Conflict */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.AudioDeviceDeleteConflict"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    setAudioDeviceGain: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description Audio device id */
                id: number;
            };
            cookie?: never;
        };
        /** @description Gain setting */
        requestBody: {
            content: {
                "application/json": components["schemas"]["dto.AudioDeviceSetGainRequest"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.AudioDeviceResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Not Found */
            404: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    playTestTone: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description Audio device id */
                id: number;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.TestToneResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Not Found */
            404: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Service Unavailable */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    login: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Credentials */
        requestBody: {
            content: {
                "application/json": components["schemas"]["webauth.LoginRequest"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    /** @description Session cookie */
                    "Set-Cookie"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webauth.StatusResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    logout: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    /** @description Cleared session cookie */
                    "Set-Cookie"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webauth.StatusResponse"];
                };
            };
        };
    };
    getSetupStatus: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webauth.SetupStatusResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    createFirstUser: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Credentials for the first administrator */
        requestBody: {
            content: {
                "application/json": components["schemas"]["webauth.SetupRequest"];
            };
        };
        responses: {
            /** @description Created */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webauth.SetupCreatedResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Forbidden */
            403: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    listBeacons: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.BeaconResponse"][];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    createBeacon: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: components["requestBodies"]["dto.BeaconRequest"];
        responses: {
            /** @description Created */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.BeaconResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    getBeacon: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description Beacon id */
                id: number;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.BeaconResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Not Found */
            404: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    updateBeacon: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description Beacon id */
                id: number;
            };
            cookie?: never;
        };
        requestBody: components["requestBodies"]["dto.BeaconRequest"];
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.BeaconResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    deleteBeacon: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description Beacon id */
                id: number;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description No Content */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    sendBeacon: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description Beacon id */
                id: number;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.BeaconSendResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Not Found */
            404: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Service Unavailable */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    listChannels: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.ChannelResponse"][];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    createChannel: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: components["requestBodies"]["dto.ChannelRequest"];
        responses: {
            /** @description Created */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.ChannelResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    getChannel: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description Channel id */
                id: number;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.ChannelResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Not Found */
            404: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    updateChannel: {
        parameters: {
            query?: {
                /** @description Force the update even if it would break existing TX referrers */
                force?: boolean;
            };
            header?: never;
            path: {
                /** @description Channel id */
                id: number;
            };
            cookie?: never;
        };
        requestBody: components["requestBodies"]["dto.ChannelRequest"];
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.ChannelResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Conflict */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webapi.ChannelReferrersResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    deleteChannel: {
        parameters: {
            query?: {
                /** @description Cascade per-table deletes / nulls; 409 without it when referrers exist */
                cascade?: boolean;
            };
            header?: never;
            path: {
                /** @description Channel id */
                id: number;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description No Content */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Not Found */
            404: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Conflict */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webapi.ChannelReferrersResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    getChannelReferrers: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description Channel id */
                id: number;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webapi.ChannelReferrersResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Not Found */
            404: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    getChannelStats: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description Channel id */
                id: number;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["modembridge.ChannelStats"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Not Found */
            404: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Service Unavailable */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    getDigipeaterConfig: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.DigipeaterConfigResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    updateDigipeaterConfig: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Digipeater config */
        requestBody: {
            content: {
                "application/json": components["schemas"]["dto.DigipeaterConfigRequest"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.DigipeaterConfigResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    listDigipeaterRules: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.DigipeaterRuleResponse"][];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    createDigipeaterRule: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: components["requestBodies"]["dto.DigipeaterRuleRequest"];
        responses: {
            /** @description Created */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.DigipeaterRuleResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    updateDigipeaterRule: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description Digipeater rule id */
                id: number;
            };
            cookie?: never;
        };
        requestBody: components["requestBodies"]["dto.DigipeaterRuleRequest"];
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.DigipeaterRuleResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    deleteDigipeaterRule: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description Digipeater rule id */
                id: number;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description No Content */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    getGps: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.GPSResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    updateGps: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description GPS config */
        requestBody: {
            content: {
                "application/json": components["schemas"]["dto.GPSRequest"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.GPSResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    listAvailableGps: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["gps.SerialPortInfo"][];
                };
            };
        };
    };
    getHealth: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.HealthResponse"];
                };
            };
        };
    };
    getIgateStatus: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["igate.Status"];
                };
            };
            /** @description Service Unavailable */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    getIgateConfig: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.IGateConfigResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    updateIgateConfig: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Igate config */
        requestBody: {
            content: {
                "application/json": components["schemas"]["dto.IGateConfigRequest"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.IGateConfigResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    listIgateFilters: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.IGateRfFilterResponse"][];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    createIgateFilter: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: components["requestBodies"]["dto.IGateRfFilterRequest"];
        responses: {
            /** @description Created */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.IGateRfFilterResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    updateIgateFilter: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description Igate RF filter id */
                id: number;
            };
            cookie?: never;
        };
        requestBody: components["requestBodies"]["dto.IGateRfFilterRequest"];
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.IGateRfFilterResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    deleteIgateFilter: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description Igate RF filter id */
                id: number;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description No Content */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    setIgateSimulation: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Desired simulation mode */
        requestBody: {
            content: {
                "application/json": components["schemas"]["webapi.IgateToggleRequest"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webapi.IgateSimulationResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Service Unavailable */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    listKiss: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.KissResponse"][];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    createKiss: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: components["requestBodies"]["dto.KissRequest"];
        responses: {
            /** @description Created */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.KissResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    getKiss: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description KISS interface id */
                id: number;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.KissResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Not Found */
            404: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    updateKiss: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description KISS interface id */
                id: number;
            };
            cookie?: never;
        };
        requestBody: components["requestBodies"]["dto.KissRequest"];
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.KissResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    deleteKiss: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description KISS interface id */
                id: number;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description No Content */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    reconnectKiss: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description KISS interface id */
                id: number;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Not Found */
            404: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Conflict */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    listMessages: {
        parameters: {
            query?: {
                /** @description Folder filter: inbox|sent|all */
                folder?: string;
                /** @description PeerCall filter */
                peer?: string;
                /** @description dm|tactical */
                thread_kind?: string;
                /** @description Exact thread key (peer callsign for DM, tactical label for tactical) */
                thread_key?: string;
                /** @description Only messages at or after this RFC3339 timestamp */
                since?: string;
                /** @description Opaque cursor from a prior response; pages forward */
                cursor?: string;
                /** @description Restrict to unread rows */
                unread_only?: boolean;
                /** @description Cap result count (1..500) */
                limit?: number;
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.MessageListResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    sendMessage: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Compose payload */
        requestBody: {
            content: {
                "application/json": components["schemas"]["dto.SendMessageRequest"];
            };
        };
        responses: {
            /** @description Accepted */
            202: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.MessageResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Service Unavailable */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    listConversations: {
        parameters: {
            query?: {
                /** @description Cap result count (1..500, default 200) */
                limit?: number;
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.ConversationSummary"][];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Service Unavailable */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    streamMessageEvents: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Server-Sent-Events stream of message changes */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "text/event-stream": string;
                };
            };
            /** @description Service Unavailable */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "text/event-stream": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    getMessagePreferences: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.MessagePreferencesResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    putMessagePreferences: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Preferences */
        requestBody: {
            content: {
                "application/json": components["schemas"]["dto.MessagePreferencesRequest"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.MessagePreferencesResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    listTacticalCallsigns: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.TacticalCallsignResponse"][];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    createTacticalCallsign: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: components["requestBodies"]["dto.TacticalCallsignRequest"];
        responses: {
            /** @description Created */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.TacticalCallsignResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Conflict */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    updateTacticalCallsign: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description Tactical id */
                id: number;
            };
            cookie?: never;
        };
        requestBody: components["requestBodies"]["dto.TacticalCallsignRequest"];
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.TacticalCallsignResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Not Found */
            404: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Conflict */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    deleteTacticalCallsign: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description Tactical id */
                id: number;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description No Content */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Not Found */
            404: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    getTacticalParticipants: {
        parameters: {
            query?: {
                /** @description Lookback window (e.g. 7d, 72h); default 7d */
                within?: string;
            };
            header?: never;
            path: {
                /** @description Tactical key (callsign label) */
                key: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.ParticipantsEnvelope"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Service Unavailable */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    deleteMessageThread: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description Thread kind (dm | tactical) */
                kind: string;
                /** @description Thread key (peer callsign for DM, tactical label for tactical) */
                key: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description No Content */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Service Unavailable */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    getMessage: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description Message id */
                id: number;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.MessageResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Not Found */
            404: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    deleteMessage: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description Message id */
                id: number;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description No Content */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Not Found */
            404: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Service Unavailable */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    markMessageRead: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description Message id */
                id: number;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description No Content */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Service Unavailable */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    resendMessage: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description Message id */
                id: number;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Accepted */
            202: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.MessageResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Not Found */
            404: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Conflict */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Service Unavailable */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    markMessageUnread: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description Message id */
                id: number;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description No Content */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Service Unavailable */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    listPackets: {
        parameters: {
            query?: {
                /** @description Only entries at or after this RFC3339 timestamp */
                since?: string;
                /** @description Filter by Entry.Source (e.g. rf, is) */
                source?: string;
                /** @description Filter by APRS packet type (Entry.Type) */
                type?: string;
                /** @description Filter by direction (RX|TX|IS) */
                direction?: string;
                /** @description Filter by channel number */
                channel?: number;
                /** @description Cap result count (non-negative) */
                limit?: number;
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webapi.packetDTO"][];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    getPosition: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webapi.PositionDTO"];
                };
            };
        };
    };
    getPositionLog: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.PositionLogResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    updatePositionLog: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Position log config */
        requestBody: {
            content: {
                "application/json": components["schemas"]["dto.PositionLogRequest"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.PositionLogResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    getMapsConfig: {
        parameters: {
            query?: {
                /** @description Set to 1 to include the device token in the response */
                include_token?: string;
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.MapsConfigResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    updateMapsConfig: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Maps preference */
        requestBody: {
            content: {
                "application/json": components["schemas"]["dto.MapsConfigRequest"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.MapsConfigResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    registerMapsToken: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Registration request */
        requestBody: {
            content: {
                "application/json": components["schemas"]["dto.RegisterRequest"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.RegisterResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Conflict */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Too Many Requests */
            429: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    getThemeConfig: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.ThemeConfigResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    updateThemeConfig: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Theme preference */
        requestBody: {
            content: {
                "application/json": components["schemas"]["dto.ThemeConfigRequest"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.ThemeConfigResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    getUnitsConfig: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.UnitsConfigResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    updateUnitsConfig: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Units preference */
        requestBody: {
            content: {
                "application/json": components["schemas"]["dto.UnitsConfigRequest"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.UnitsConfigResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    listPttConfigs: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.PttResponse"][];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    upsertPttConfig: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: components["requestBodies"]["dto.PttRequest"];
        responses: {
            /** @description Created */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.PttResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    listPttDevices: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["pttdevice.AvailableDevice"][];
                };
            };
        };
    };
    getPttCapabilities: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webapi.pttCapabilities"];
                };
            };
        };
    };
    listGpioLines: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description URL-encoded gpiochip device path (e.g. %2Fdev%2Fgpiochip0) */
                chip: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["pttdevice.GpioLineInfo"][];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Forbidden */
            403: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Not Found */
            404: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Not Implemented */
            501: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    testRigctld: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description rigctld endpoint */
        requestBody: {
            content: {
                "application/json": components["schemas"]["dto.TestRigctldRequest"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.TestRigctldResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    getPttConfig: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description Channel id */
                channel: number;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.PttResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Not Found */
            404: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    updatePttConfig: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description Channel id */
                channel: number;
            };
            cookie?: never;
        };
        requestBody: components["requestBodies"]["dto.PttRequest"];
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.PttResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    deletePttConfig: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description Channel id */
                channel: number;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description No Content */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "*/*": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    listReleaseNotes: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.ReleaseNotesResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    ackReleaseNotes: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description No Content */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    listUnseenReleaseNotes: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.ReleaseNotesResponse"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    getSmartBeacon: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.SmartBeaconConfigResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    updateSmartBeacon: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description SmartBeacon configuration */
        requestBody: {
            content: {
                "application/json": components["schemas"]["dto.SmartBeaconConfigRequest"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.SmartBeaconConfigResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    getStationConfig: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.StationConfigResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    updateStationConfig: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Station config */
        requestBody: {
            content: {
                "application/json": components["schemas"]["dto.StationConfigRequest"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.StationConfigResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    listStations: {
        parameters: {
            query: {
                /** @description Bounding box as sw_lat,sw_lon,ne_lat,ne_lon */
                bbox: string;
                /** @description Lookback window in seconds (default 3600) */
                timerange?: number;
                /** @description Delta mode: only stations heard at or after this RFC3339Nano timestamp */
                since?: string;
                /** @description Comma-separated extras (currently: weather) */
                include?: string;
            };
            header?: {
                /** @description ETag from a prior response; returns 304 on match */
                "If-None-Match"?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webapi.StationDTO"][];
                };
            };
            /** @description Not Modified */
            304: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    autocompleteStations: {
        parameters: {
            query?: {
                /** @description Prefix (case-insensitive). Empty returns bots + recent stations. */
                q?: string;
                /** @description Cap result count (1..100, default 25) */
                limit?: number;
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.StationAutocomplete"][];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    getStatus: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webapi.StatusDTO"];
                };
            };
        };
    };
    acceptTacticalInvite: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Tactical + optional source message id */
        requestBody: {
            content: {
                "application/json": components["schemas"]["dto.AcceptInviteRequest"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.AcceptInviteResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Service Unavailable */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    listTxTiming: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.TxTimingResponse"][];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    createTxTiming: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: components["requestBodies"]["dto.TxTimingRequest"];
        responses: {
            /** @description Created */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.TxTimingResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    getTxTiming: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description Channel id */
                id: number;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.TxTimingResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Not Found */
            404: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    updateTxTiming: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description Channel id */
                id: number;
            };
            cookie?: never;
        };
        requestBody: components["requestBodies"]["dto.TxTimingRequest"];
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.TxTimingResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    getUpdatesConfig: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.UpdatesConfigResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    updateUpdatesConfig: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Updates configuration */
        requestBody: {
            content: {
                "application/json": components["schemas"]["dto.UpdatesConfigRequest"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.UpdatesConfigResponse"];
                };
            };
            /** @description Bad Request */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webtypes.ErrorResponse"];
                };
            };
        };
    };
    getUpdatesStatus: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["dto.UpdatesStatusResponse"];
                };
            };
        };
    };
    getVersion: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["webapi.VersionResponse"];
                };
            };
        };
    };
}
