# Auth

The `auth` module handles authorization and permissions for the bot. The auth module uses the same SQL database as the whatsmeow module for data persistence. It makes sure that:

- Only registered users can issue commands
- Groups must be registered before responding to commands
- User permissions are enforced through assigned ranks

## Data model

**User**: Individual allowed to interact with bot:

| Field          | Type        | Description                                        |
| -------------- | ----------- | -------------------------------------------------- |
| `ID`           | `string`    | Unique identifier of the user (WhatsApp JID)       |
| `Rank`         | `string`    | Name of the assigned rank (determines permissions) |
| `RegisteredAt` | `timestamp` | When the user was registered                       |
| `RegisteredBy` | `string`    | ID of the user who registered this user            |

**Group**: WhatsApp group where bot is active (unregistered groups ignore all commands except registration by admins):

| Field          | Type        | Description                              |
| -------------- | ----------- | ---------------------------------------- |
| `ID`           | `string`    | Unique identifier of the group           |
| `RegisteredAt` | `timestamp` | When the group was registered            |
| `RegisteredBy` | `string`    | ID of the user who registered this group |

**Rank**: Permission level configuration:

| Field      | Type       | Description                                                |
| ---------- | ---------- | ---------------------------------------------------------- |
| `Name`     | `string`   | Unique name of the rank                                    |
| `Level`    | `int`      | Numeric value for ordering ranks (lower = higher priority) |
| `Commands` | `[]string` | List of command names this rank can execute                |

**Default ranks** (defined in [schema.go](schema.go?plain=1#L44)):

| Name    | Level | Commands                                           | Description       |
| ------- | ----- | -------------------------------------------------- | ----------------- |
| `owner` | 0     | `*`                                                | Full access       |
| `admin` | 10    | `help`, `latex`, `register_user`, `register_group` | Management access |
| `user`  | 100   | `help`, `latex`                                    | Basic access      |

Database tables (`users`, `ranks`, `registered_groups`) automatically created the first time the application starts.

## API Reference

**Service initialization**

```go
db, _ := sql.Open("sqlite3", "./bot.db")
authService := auth.New(db)
```

**CheckPermission(ctx, userID, groupID, command)** -> `(PermissionResult, error)`: Verifies if user can execute command in context:

1. Validates input
2. Confirms user registration
3. Checks group registration (if provided)
4. Verifies command against user's rank

Example usage:

```go
result, _ := authService.CheckPermission(ctx,
    "7778889999@s.whatsapp.net",
    "12036304@g.us",
    "latex"
)

fmt.Println(result.Allowed, result.Reason)
// Outputs: true, "Access granted"
// or false, "Command not allowed for your rank"
```

**RegisterUser(ctx, userID, rank, registeredBy)** -> `error`: Adds new authorized user:

Example usage:

```go
err := authService.RegisterUser(ctx,
    "1231231234@s.whatsapp.net",
    "user",
    "4445556666@s.whatsapp.net"
)
```

**RegisterGroup(ctx, groupID, registeredBy)** -> `error`: Authorizes bot operation in group.

Example usage:

```go
err := authService.RegisterGroup(ctx,
    "4567890123@g.us",
    "4445556666@s.whatsapp.net"
)
```
