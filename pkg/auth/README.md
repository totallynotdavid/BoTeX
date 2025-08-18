# Auth

The auth module is the component responsible for managing all authorization and permission-related logic in the bot system. It ensures that users can only execute commands appropriate for their assigned roles (known as ranks in the system). Its main responsabilities are:

- Verify if a user has the right to run a specific command, both in private chats and in groups
- Manage the list of registered users who are allowed to interact with the bot
- Manage registered groups where the bot can operate

> [!NOTE]
> The auth module uses the same SQL database as the whatsmeow module for data persistence.

## Data Model

### User

Represents an individual who can interact with the bot.

| Field          | Type        | Description                                        |
| -------------- | ----------- | -------------------------------------------------- |
| `ID`           | `string`    | Unique identifier of the user (WhatsApp JID)       |
| `Rank`         | `string`    | Name of the assigned rank (determines permissions) |
| `RegisteredAt` | `timestamp` | When the user was registered                       |
| `RegisteredBy` | `string`    | ID of the user who registered this user            |

### Group

Represents a registered WhatsApp group where the bot is active.

| Field          | Type        | Description                              |
| -------------- | ----------- | ---------------------------------------- |
| `ID`           | `string`    | Unique identifier of the group           |
| `RegisteredAt` | `timestamp` | When the group was registered            |
| `RegisteredBy` | `string`    | ID of the user who registered this group |

> [!IMPORTANT]
> If a group is not registered, commands (except for registration commands used by admins) will be ignored.

### Rank

Represents a level of permission assigned to a user.

| Field      | Type       | Description                                                |
| ---------- | ---------- | ---------------------------------------------------------- |
| `Name`     | `string`   | Unique name of the rank                                    |
| `Level`    | `int`      | Numeric value for ordering ranks (lower = higher priority) |
| `Commands` | `[]string` | List of command names this rank can execute                |

> [!TIP]
> Use the special wildcard command `"*"` to allow all commands for a rank.

[Back to top](#top)

## Database Schema

The module relies on three main tables in the SQLite database:

- `users`
- `ranks`
- `registered_groups`

> [!NOTE]
> The schema is automatically initialized on startup.

### Default Ranks

The system comes with a set of predefined ranks:

| Name    | Level | Commands                                           | Description                          |
| ------- | ----- | -------------------------------------------------- | ------------------------------------ |
| `owner` | 0     | `*`                                                | Bot owner with full access           |
| `admin` | 10    | `help`, `latex`, `register_user`, `register_group` | Administrator with management access |
| `user`  | 100   | `help`, `latex`                                    | Basic user access                    |

[Back to top](#top)

## API Reference

The auth module exposes its functionality through the `Auth` interface. The `Service` struct provides the concrete implementation.

### Constructor

#### `New(db *sql.DB) *Service`

Creates a new `Service` instance with access to all authentication and authorization methods.

**Parameters:**

- `db`: SQLite database connection

**Returns:**

- `*Service`: New auth service instance

**Example:**

```go
package main

import (
    "database/sql"
    "botex/pkg/auth"
    _ "github.com/mattn/go-sqlite3"
)

func main() {
    db, err := sql.Open("sqlite3", "./bot.db")
    if err != nil {
        // Handle error
    }

    // Create a new auth service
    authService := auth.New(db)

    // Now you can use authService methods
}
```

### Core Methods

#### `CheckPermission`

```go
CheckPermission(ctx context.Context, userID, groupID, command string) (*PermissionResult, error)
```

> [!IMPORTANT]
> This is the most critical method. It checks if a user is allowed to execute a given command.

**Parameters:**

- `userID` (`string`): Unique ID of the user trying to run the command
- `groupID` (`string`): ID of the group where the command is being run (empty string `""` for private messages)
- `command` (`string`): Name of the command being checked (e.g., `"latex"`)

**Returns:**

- `*PermissionResult`: Struct detailing whether access is allowed and why
- `error`: Database-level errors (permission denial is indicated by `Allowed: false`, not an error)

**Logic Flow:**

1. Validates the command format
2. Checks if the user is registered (denies if not)
3. If `groupID` provided, checks if the group is registered (denies if not)
4. Retrieves the user's assigned rank
5. Checks if the rank's command list includes the command or wildcard `*`
6. Returns the result

#### `RegisterUser`

```go
RegisterUser(ctx context.Context, userID, rank, registeredBy string) error
```

Registers a new user with a specified rank.

**Parameters:**

- `userID` (`string`): ID of the new user to register
- `rank` (`string`): Name of the rank to assign (must exist in `ranks` table)
- `registeredBy` (`string`): ID of the user performing this action (for auditing)

**Returns:**

- `nil`: Success
- `ErrUserExists`: User is already registered
- `ErrRankNotFound`: Specified rank does not exist
- `ErrInvalidInput`: Invalid parameters provided

#### `RegisterGroup`

```go
RegisterGroup(ctx context.Context, groupID, registeredBy string) error
```

Registers a new group, allowing the bot to respond to commands within it.

**Parameters:**

- `groupID` (`string`): ID of the group to register
- `registeredBy` (`string`): ID of the user performing the registration (must be registered)

**Returns:**

- `nil`: Success
- `ErrGroupExists`: Group is already registered
- `ErrUserNotFound`: `registeredBy` user does not exist
- `ErrInvalidInput`: Invalid parameters provided

[Back to top](#top)

## Examples

### Checking Permissions

```go
userID := "7778889999@s.whatsapp.net" // A registered 'user'
groupID := "12036304@g.us" // A registered group

// ✅ Check permission for a command the user has
result, err := authService.CheckPermission(context.Background(), userID, groupID, "latex")
if err != nil {
    log.Fatalf("Permission check failed: %v", err)
}
fmt.Printf("Can user run 'latex'? Allowed: %t, Reason: %s\n", result.Allowed, result.Reason)
// Output: Can user run 'latex'? Allowed: true, Reason: Access granted

// ❌ Check permission for a command the user does NOT have
result, err = authService.CheckPermission(context.Background(), userID, groupID, "register_user")
if err != nil {
    log.Fatalf("Permission check failed: %v", err)
}
fmt.Printf("Can user run 'register_user'? Allowed: %t, Reason: %s\n", result.Allowed, result.Reason)
// Output: Can user run 'register_user'? Allowed: false, Reason: Command not allowed for your rank
```

### Registering Users

```go
adminID := "4445556666@s.whatsapp.net"
newUserID := "1231231234@s.whatsapp.net"

err := authService.RegisterUser(context.Background(), newUserID, "user", adminID)
if err != nil {
    if errors.Is(err, auth.ErrUserExists) {
        fmt.Println("User is already registered.")
    } else {
        log.Fatalf("Failed to register user: %v", err)
    }
} else {
    fmt.Println("User registered successfully!")
}
```

### Registering Groups

```go
adminID := "4445556666@s.whatsapp.net"
newGroupID := "4567890123@g.us"

err := authService.RegisterGroup(context.Background(), newGroupID, adminID)
if err != nil {
    log.Fatalf("Failed to register group: %v", err)
} else {
    fmt.Println("Group registered successfully!")
}
```

> [!WARNING]
> Always handle errors appropriately when calling auth methods. Database operations can fail, and permission checks should be performed before executing sensitive commands.
