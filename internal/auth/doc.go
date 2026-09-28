// Package auth decides which users may run which commands. Users, ranks and
// registered groups live in the same SQLite database as the WhatsApp session.
//
// A user is a WhatsApp JID with a rank. A rank has a level, where lower means
// more privileged, and a list of command names, where "*" means every command.
// The owner rank always exists and holds "*"; the app passes its other ranks to
// [New].
//
// [Service.CheckPermission] allows a command when the user is registered and
// the user's rank lists it. [Service.SeedOwners] registers the JIDs from
// BOTEX_OWNER_JIDS as owners on every start, without touching a user that
// already exists: one with another rank is reported as skipped and a
// deactivated one as inactive.
package auth
