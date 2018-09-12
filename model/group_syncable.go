// Copyright (c) 2018-present Mattermost, Inc. All Rights Reserved.
// See License.txt for license information.

package model

import (
	"net/http"
)

type GroupSyncableType int

const (
	GSTeam GroupSyncableType = iota
	GSChannel
)

func (gst GroupSyncableType) String() string {
	return [...]string{"Team", "Channel"}[gst]
}

type GroupSyncable struct {
	GroupId    string            `json:"group_id"`
	SyncableId string            `db:"-" json:"syncable_id"`
	CanLeave   bool              `json:"can_leave"`
	AutoAdd    bool              `json:"auto_add"`
	CreateAt   int64             `json:"create_at"`
	DeleteAt   int64             `json:"delete_at"`
	UpdateAt   int64             `json:"update_at"`
	Type       GroupSyncableType `db:"-" json:"type"`
}

func (syncable *GroupSyncable) IsValid() *AppError {
	if !IsValidId(syncable.GroupId) {
		return NewAppError("GroupSyncable.SyncableIsValid", "model.group_syncable.group_id.app_error", nil, "", http.StatusBadRequest)
	}
	if !IsValidId(syncable.SyncableId) {
		return NewAppError("GroupSyncable.SyncableIsValid", "model.group_syncable.syncable_id.app_error", nil, "", http.StatusBadRequest)
	}
	if syncable.AutoAdd == false && syncable.CanLeave == false {
		return NewAppError("GroupSyncable.SyncableIsValid", "model.group_syncable.invalid_state", nil, "", http.StatusBadRequest)
	}
	return nil
}
