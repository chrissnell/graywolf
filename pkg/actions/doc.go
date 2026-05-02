// Package actions implements the Graywolf Actions subsystem: an APRS
// message-driven trigger surface that dispatches operator-defined
// commands and webhooks. Inbound messages prefixed `@@` and addressed
// to the trigger surface are diverted from the messages router by the
// classifier, parsed, OTP-verified (when required), and dispatched
// through a per-Action FIFO runner to one of the registered Executors.
// Every attempt is recorded in the action_invocations audit log and a
// reply is sent back to the originator over the matching transport.
package actions
