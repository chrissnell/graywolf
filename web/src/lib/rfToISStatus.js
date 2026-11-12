// Connectivity alone never implies that RF uploads are enabled.
export function rfToISStatus(igate, fresh = true) {
 if (!fresh) return {label: 'Status unavailable', tone: 'unknown'};
 if (!igate) return {label: 'Disabled', tone: 'disabled'};
 if (typeof igate.rf_to_is_enabled !== 'boolean') return {label: 'Status unavailable', tone: 'unknown'};
 if (!igate.rf_to_is_enabled) return {label: 'Disabled', tone: 'disabled'};
 if (!igate.connected) return {label: 'IS disconnected', tone: 'pending'};
 if (igate.simulation_mode) return {label: 'Simulation', tone: 'simulation'};
 return {label: 'Active · filtered', tone: 'active'};
}
