package model

// Msg8701 downloads recorder parameters. It shares the same wire format as
// MsgRecordCommand (0x8700), so it is an alias of that type.
type Msg8701 = MsgRecordCommand
