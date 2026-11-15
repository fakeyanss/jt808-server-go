package api

import "errors"

func (r postDeviceControlRequest) validate() error {
	if r.CommandWord != 1 && r.CommandWord != 2 && r.CommandWord != 4 && r.CommandWord != 5 {
		return errors.New("supported commandWord values are 1 (upgrade), 2 (connect server), 4 (reset), and 5 (factory reset)")
	}
	return nil
}

func (r postDeviceRecorderCommandRequest) validate() error {
	if r.MessageID != 0x8700 && r.MessageID != 0x8701 {
		return errors.New("messageId must be 0x8700 or 0x8701")
	}
	return nil
}

func (r postDeviceMediaCommandRequest) validate() error {
	if r.MessageID != 0x8803 && r.MessageID != 0x8804 && r.MessageID != 0x8805 {
		return errors.New("messageId must be 0x8803, 0x8804, or 0x8805")
	}
	return nil
}

func (r postDeviceRegionsDeleteRequest) validate() error {
	if r.MessageID != 0x8601 && r.MessageID != 0x8603 && r.MessageID != 0x8605 && r.MessageID != 0x8607 {
		return errors.New("messageId must be 0x8601, 0x8603, 0x8605, or 0x8607")
	}
	if len(r.IDs) > 125 {
		return errors.New("at most 125 IDs are allowed per message")
	}
	return nil
}

func (r postDeviceRegionsSetRequest) validate() error {
	if r.MessageID != 0x8600 && r.MessageID != 0x8602 && r.MessageID != 0x8604 && r.MessageID != 0x8606 {
		return errors.New("messageId must be 0x8600, 0x8602, 0x8604, or 0x8606")
	}
	return nil
}

func (r postDeviceRegionsQueryRequest) validate() error {
	if len(r.IDs) > 125 {
		return errors.New("at most 125 IDs are allowed per query")
	}
	return nil
}

func (r postDeviceFtp1078UploadRequest) validate() error {
	if r.StartTime == nil || r.EndTime == nil || !r.EndTime.After(*r.StartTime) {
		return errors.New("startTime and endTime are required and endTime must be after startTime")
	}
	if len([]byte(r.UploadPath)) > 255 {
		return errors.New("uploadPath must be at most 255 bytes")
	}
	return nil
}

func (r postDeviceFtp1078ControlRequest) validate() error {
	if r.Action > 2 {
		return errors.New("action must be 0 (pause), 1 (resume) or 2 (cancel)")
	}
	return nil
}
