// Copyright (c) 2018-present Mattermost, Inc. All Rights Reserved.
// See License.txt for license information.

package sqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"

	"github.com/mattermost/gorp"
	"github.com/mattermost/mattermost-server/model"
	"github.com/mattermost/mattermost-server/store"
)

type GroupTeam struct {
	model.GroupSyncable
	TeamId string `db:"TeamId"`
}

type GroupChannel struct {
	model.GroupSyncable
	ChannelId string `db:"ChannelId"`
}

func initSqlSupplierGroups(sqlStore SqlStore) {
	for _, db := range sqlStore.GetAllConns() {
		groups := db.AddTableWithName(model.Group{}, "Groups").SetKeys(false, "Id")
		groups.ColMap("Id").SetMaxSize(26)
		groups.ColMap("Name").SetMaxSize(model.GroupNameMaxLength).SetUnique(true)
		groups.ColMap("DisplayName").SetMaxSize(model.GroupDisplayNameMaxLength)
		groups.ColMap("Description").SetMaxSize(model.GroupDescriptionMaxLength)
		groups.ColMap("Type").SetMaxSize(model.GroupTypeMaxLength)
		groups.ColMap("RemoteId").SetMaxSize(model.GroupRemoteIdMaxLength)

		groupMembers := db.AddTableWithName(model.GroupMember{}, "GroupMembers").SetKeys(false, "GroupId", "UserId")
		groupMembers.ColMap("GroupId").SetMaxSize(26)
		groupMembers.ColMap("UserId").SetMaxSize(26)

		groupTeams := db.AddTableWithName(GroupTeam{}, "GroupTeams").SetKeys(false, "GroupId", "TeamId")
		groupTeams.ColMap("GroupId").SetMaxSize(26)
		groupTeams.ColMap("TeamId").SetMaxSize(26)

		groupChannels := db.AddTableWithName(GroupChannel{}, "GroupChannels").SetKeys(false, "GroupId", "ChannelId")
		groupChannels.ColMap("GroupId").SetMaxSize(26)
		groupChannels.ColMap("ChannelId").SetMaxSize(26)
	}
}

func (s *SqlSupplier) GroupCreate(ctx context.Context, group *model.Group, hints ...store.LayeredStoreHint) *store.LayeredStoreSupplierResult {
	result := store.NewSupplierResult()

	if len(group.Id) != 0 {
		result.Err = model.NewAppError("SqlGroupStore.GroupCreate", "store.sql_group.create.invalid_group_id", nil, "", http.StatusBadRequest)
		return result
	}

	if err := group.IsValidForCreate(); err != nil {
		result.Err = err
		return result
	}

	var transaction *gorp.Transaction
	var tErr error
	if transaction, tErr = s.GetMaster().Begin(); tErr != nil {
		result.Err = model.NewAppError("SqlGroupStore.GroupCreate", "store.sql_group.create.begin_transaction_error", nil, tErr.Error(), http.StatusInternalServerError)
		return result
	}

	if err := group.IsValidForCreate(); err != nil {
		result.Err = err
		return result
	}

	group.Id = model.NewId()
	group.CreateAt = model.GetMillis()
	group.UpdateAt = group.CreateAt

	if err := transaction.Insert(group); err != nil {
		if IsUniqueConstraintError(err, []string{"Name", "groups_name_key"}) {
			result.Err = model.NewAppError("SqlGroupStore.GroupCreate", "store.sql_group.create.unique_constraint", nil, err.Error(), http.StatusInternalServerError)
		} else {
			result.Err = model.NewAppError("SqlGroupStore.GroupCreate", "store.sql_group.create.insert_error", nil, err.Error(), http.StatusInternalServerError)
		}
		transaction.Rollback()
	} else {
		result.Data = group
	}

	if err := transaction.Commit(); err != nil {
		result.Err = model.NewAppError("SqlGroupStore.GroupCreate", "store.sql_group.create.commit_error", nil, err.Error(), http.StatusInternalServerError)
		result.Data = nil
	}

	return result
}

func (s *SqlSupplier) GroupGet(ctx context.Context, groupId string, hints ...store.LayeredStoreHint) *store.LayeredStoreSupplierResult {
	result := store.NewSupplierResult()

	var group *model.Group
	if err := s.GetReplica().SelectOne(&group, "SELECT * from Groups WHERE Id = :Id", map[string]interface{}{"Id": groupId}); err != nil {
		if err == sql.ErrNoRows {
			result.Err = model.NewAppError("SqlGroupStore.Get", "store.sql_group.get.no_rows", nil, err.Error(), http.StatusInternalServerError)
		} else {
			result.Err = model.NewAppError("SqlGroupStore.Get", "store.sql_group.get.select_error", nil, err.Error(), http.StatusInternalServerError)
		}
		return result
	}

	result.Data = group
	return result
}

func (s *SqlSupplier) GroupGetAllPage(ctx context.Context, offset int, limit int, hints ...store.LayeredStoreHint) *store.LayeredStoreSupplierResult {
	result := store.NewSupplierResult()

	var groups []*model.Group

	if _, err := s.GetReplica().Select(&groups, "SELECT * from Groups WHERE DeleteAt = 0 ORDER BY CreateAt DESC LIMIT :Limit OFFSET :Offset", map[string]interface{}{"Limit": limit, "Offset": offset}); err != nil {
		if err != sql.ErrNoRows {
			result.Err = model.NewAppError("SqlGroupStore.GetAllPage", "store.sql_group.get_all_page.select_error", nil, err.Error(), http.StatusInternalServerError)
			return result
		}
	}

	result.Data = groups

	return result
}

func (s *SqlSupplier) GroupUpdate(ctx context.Context, group *model.Group, hints ...store.LayeredStoreHint) *store.LayeredStoreSupplierResult {
	result := store.NewSupplierResult()

	var retrievedGroup *model.Group
	if err := s.GetMaster().SelectOne(&retrievedGroup, "SELECT * FROM Groups WHERE Id = :Id", map[string]interface{}{"Id": group.Id}); err != nil {
		if err == sql.ErrNoRows {
			result.Err = model.NewAppError("SqlGroupStore.GroupUpdate", "store.sql_group.update.no_rows", nil, "id="+group.Id+","+err.Error(), http.StatusNotFound)
		} else {
			result.Err = model.NewAppError("SqlGroupStore.GroupUpdate", "store.sql_group.update.select_error", nil, "id="+group.Id+","+err.Error(), http.StatusInternalServerError)
		}
		return result
	}

	// Reset these properties, don't update them based on input
	group.DeleteAt = retrievedGroup.DeleteAt
	group.CreateAt = retrievedGroup.CreateAt
	group.UpdateAt = model.GetMillis()

	if err := group.IsValidForUpdate(); err != nil {
		result.Err = err
		return result
	}

	rowsChanged, err := s.GetMaster().Update(group)
	if err != nil {
		result.Err = model.NewAppError("SqlGroupStore.GroupUpdate", "store.sql_group.update.update_error", nil, err.Error(), http.StatusInternalServerError)
		return result
	}
	if rowsChanged != 1 {
		result.Err = model.NewAppError("SqlGroupStore.GroupUpdate", "store.sql_group.update.no_rows_changed", nil, "", http.StatusInternalServerError)
		return result
	}

	result.Data = group
	return result
}

func (s *SqlSupplier) GroupDelete(ctx context.Context, groupID string, hints ...store.LayeredStoreHint) *store.LayeredStoreSupplierResult {
	result := store.NewSupplierResult()

	if !model.IsValidId(groupID) {
		result.Err = model.NewAppError("SqlGroupStore.Delete", "store.sql_group.delete.invalid_group_id", nil, "Id="+groupID, http.StatusBadRequest)
	}

	var group *model.Group
	if err := s.GetReplica().SelectOne(&group, "SELECT * from Groups WHERE Id = :Id", map[string]interface{}{"Id": groupID}); err != nil {
		if err == sql.ErrNoRows {
			result.Err = model.NewAppError("SqlGroupStore.Delete", "store.sql_group.delete.no_rows", nil, "Id="+groupID+", "+err.Error(), http.StatusNotFound)
		} else {
			result.Err = model.NewAppError("SqlGroupStore.Delete", "store.sql_group.delete.select_error", nil, err.Error(), http.StatusInternalServerError)
		}

		return result
	}

	if group.DeleteAt != 0 {
		result.Err = model.NewAppError("SqlGroupStore.Delete", "store.sql_group.delete.already_deleted", nil, "group_id="+groupID, http.StatusInternalServerError)
		return result
	}

	time := model.GetMillis()
	group.DeleteAt = time
	group.UpdateAt = time

	if rowsChanged, err := s.GetMaster().Update(group); err != nil {
		result.Err = model.NewAppError("SqlGroupStore.Delete", "store.sql_group.delete.update_error", nil, err.Error(), http.StatusInternalServerError)
	} else if rowsChanged != 1 {
		result.Err = model.NewAppError("SqlGroupStore.Delete", "store.sql_group.delete.no_rows_affected", nil, "no record to update", http.StatusInternalServerError)
	} else {
		result.Data = group
	}

	return result
}

func (s *SqlSupplier) GroupCreateMember(ctx context.Context, groupID string, userID string, hints ...store.LayeredStoreHint) *store.LayeredStoreSupplierResult {
	result := store.NewSupplierResult()

	member := &model.GroupMember{
		GroupId:  groupID,
		UserId:   userID,
		CreateAt: model.GetMillis(),
	}

	if result.Err = member.IsValid(); result.Err != nil {
		return result
	}

	if err := s.GetMaster().Insert(member); err != nil {
		if IsUniqueConstraintError(err, []string{"GroupId", "UserId", "groupmembers_pkey", "PRIMARY"}) {
			result.Err = model.NewAppError("SqlGroupStore.CreateMember", "store.sql_group.create_member.unique_error", nil, "group_id="+member.GroupId+", user_id="+member.UserId+", "+err.Error(), http.StatusBadRequest)
			return result
		}
		result.Err = model.NewAppError("SqlGroupStore.CreateMember", "store.sql_group.create_member.save.insert_error", nil, "group_id="+member.GroupId+", user_id="+member.UserId+", "+err.Error(), http.StatusInternalServerError)
		return result
	}

	var retrievedMember *model.GroupMember
	if err := s.GetMaster().SelectOne(&retrievedMember, "SELECT * FROM GroupMembers WHERE GroupId = :GroupId AND UserId = :UserId", map[string]interface{}{"GroupId": member.GroupId, "UserId": member.UserId}); err != nil {
		if err == sql.ErrNoRows {
			result.Err = model.NewAppError("SqlGroupStore.CreateMember", "store.sql_group.create_member.no_rows", nil, "group_id="+member.GroupId+"user_id="+member.UserId+","+err.Error(), http.StatusNotFound)
		} else {
			result.Err = model.NewAppError("SqlGroupStore.CreateMember", "store.sql_group.create_member.select_error", nil, "group_id="+member.GroupId+"user_id="+member.UserId+","+err.Error(), http.StatusInternalServerError)
		}
		return result
	}
	result.Data = retrievedMember
	return result
}

func (s *SqlSupplier) GroupDeleteMember(ctx context.Context, groupID string, userID string, hints ...store.LayeredStoreHint) *store.LayeredStoreSupplierResult {
	result := store.NewSupplierResult()

	if !model.IsValidId(groupID) {
		result.Err = model.NewAppError("SqlGroupStore.DeleteMember", "store.sql_group.delete_member.invalid_group_id", nil, "", http.StatusBadRequest)
		return result
	}
	if !model.IsValidId(userID) {
		result.Err = model.NewAppError("SqlGroupStore.DeleteMember", "store.sql_group.delete_member.invalid_user_id", nil, "", http.StatusBadRequest)
		return result
	}

	var retrievedMember *model.GroupMember
	if err := s.GetMaster().SelectOne(&retrievedMember, "SELECT * FROM GroupMembers WHERE GroupId = :GroupId AND UserId = :UserId", map[string]interface{}{"GroupId": groupID, "UserId": userID}); err != nil {
		if err == sql.ErrNoRows {
			result.Err = model.NewAppError("SqlGroupStore.DeleteMember", "store.sql_group.delete_member.no_rows", nil, "group_id="+groupID+"user_id="+userID+","+err.Error(), http.StatusNotFound)
			return result
		}
		result.Err = model.NewAppError("SqlGroupStore.DeleteMember", "store.sql_group.delete_member.select_error", nil, "group_id="+groupID+"user_id="+userID+","+err.Error(), http.StatusInternalServerError)
		return result
	}

	if retrievedMember.DeleteAt != 0 {
		result.Err = model.NewAppError("SqlGroupStore.DeleteMember", "store.sql_group.delete_member.already_deleted", nil, "group_id="+groupID+"user_id="+userID, http.StatusInternalServerError)
		return result
	}

	retrievedMember.DeleteAt = model.GetMillis()

	if rowsChanged, err := s.GetMaster().Update(retrievedMember); err != nil {
		result.Err = model.NewAppError("SqlGroupStore.DeleteMember", "store.sql_scheme.delete_member.update_error", nil, err.Error(), http.StatusInternalServerError)
		return result
	} else if rowsChanged != 1 {
		result.Err = model.NewAppError("SqlGroupStore.DeleteMember", "store.sql_scheme.delete_member.no_rows_affected", nil, "no record to update", http.StatusInternalServerError)
		return result
	}

	result.Data = retrievedMember
	return result
}

func (s *SqlSupplier) GroupCreateGroupSyncable(ctx context.Context, groupSyncable *model.GroupSyncable, hints ...store.LayeredStoreHint) *store.LayeredStoreSupplierResult {
	result := store.NewSupplierResult()

	if err := groupSyncable.IsValid(); err != nil {
		result.Err = err
		return result
	}

	// Reset values that shouldn't be updatable by parameter
	groupSyncable.DeleteAt = 0
	groupSyncable.CreateAt = model.GetMillis()
	groupSyncable.UpdateAt = groupSyncable.CreateAt

	var err error

	switch groupSyncable.Type {
	case model.GSTeam:
		err = s.GetMaster().Insert(&GroupTeam{
			*groupSyncable,
			groupSyncable.SyncableId,
		})
	case model.GSChannel:
		err = s.GetMaster().Insert(&GroupChannel{
			*groupSyncable,
			groupSyncable.SyncableId,
		})
	default:
		model.NewAppError("SqlGroupStore.CreateGroupSyncable", "store.sql_group.create_group_syncable.invalid_syncable_type", nil, "group_id="+groupSyncable.GroupId+", syncable_id="+groupSyncable.SyncableId+", "+err.Error(), http.StatusInternalServerError)
		return result
	}
	if err != nil {
		if err == sql.ErrNoRows {
			result.Err = model.NewAppError("SqlGroupStore.CreateGroupSyncable", "store.sql_group.create_group_syncable.no_rows_affected", nil, "group_id="+groupSyncable.GroupId+", syncable_id="+groupSyncable.SyncableId, http.StatusInternalServerError)
		}
		result.Err = model.NewAppError("SqlGroupStore.CreateGroupSyncable", "store.sql_group.create_group_syncable.insert_error", nil, "group_id="+groupSyncable.GroupId+", syncable_id="+groupSyncable.SyncableId+", "+err.Error(), http.StatusInternalServerError)
		return result
	}

	result.Data = groupSyncable
	return result
}

func (s *SqlSupplier) GroupGetGroupSyncable(ctx context.Context, groupID string, syncableID string, syncableType model.GroupSyncableType, hints ...store.LayeredStoreHint) *store.LayeredStoreSupplierResult {
	result := store.NewSupplierResult()

	groupSyncable, err := s.getGroupSyncable(groupID, syncableID, syncableType)
	if err != nil {
		if err != sql.ErrNoRows {
			result.Err = model.NewAppError("SqlGroupStore.Get", "store.sql_group.get_group_syncable.select_error", nil, err.Error(), http.StatusInternalServerError)
			return result
		}
	}

	result.Data = groupSyncable

	return result
}

func (s *SqlSupplier) getGroupSyncable(groupID string, syncableID string, syncableType model.GroupSyncableType) (*model.GroupSyncable, error) {
	var err error
	var getResult interface{}

	switch syncableType {
	case model.GSTeam:
		getResult, err = s.GetMaster().Get(GroupTeam{}, groupID, syncableID)
	case model.GSChannel:
		getResult, err = s.GetMaster().Get(GroupChannel{}, groupID, syncableID)
	default:
	}
	if err != nil {
		return nil, err
	}

	if getResult == nil {
		return nil, sql.ErrNoRows
	}

	groupSyncable := model.GroupSyncable{}
	switch syncableType {
	case model.GSTeam:
		groupTeam := getResult.(*GroupTeam)
		groupSyncable.SyncableId = groupTeam.TeamId
		groupSyncable.GroupId = groupTeam.GroupId
		groupSyncable.CanLeave = groupTeam.CanLeave
		groupSyncable.AutoAdd = groupTeam.AutoAdd
		groupSyncable.CreateAt = groupTeam.CreateAt
		groupSyncable.DeleteAt = groupTeam.DeleteAt
		groupSyncable.UpdateAt = groupTeam.UpdateAt
		groupSyncable.Type = groupTeam.Type
	case model.GSChannel:
		groupChannel := getResult.(*GroupChannel)
		groupSyncable.SyncableId = groupChannel.ChannelId
		groupSyncable.GroupId = groupChannel.GroupId
		groupSyncable.CanLeave = groupChannel.CanLeave
		groupSyncable.AutoAdd = groupChannel.AutoAdd
		groupSyncable.CreateAt = groupChannel.CreateAt
		groupSyncable.DeleteAt = groupChannel.DeleteAt
		groupSyncable.UpdateAt = groupChannel.UpdateAt
		groupSyncable.Type = groupChannel.Type
	default:
		return nil, fmt.Errorf("unable to convert syncableType: %s", syncableType.String())
	}

	return &groupSyncable, nil
}

func (s *SqlSupplier) GroupGetAllGroupSyncablesByGroupPage(ctx context.Context, groupID string, syncableType model.GroupSyncableType, offset int, limit int, hints ...store.LayeredStoreHint) *store.LayeredStoreSupplierResult {
	result := store.NewSupplierResult()

	type GroupSyncableScanner struct {
		model.GroupSyncable
		TeamId    string
		ChannelId string
	}

	var groupSyncableScanners []*GroupSyncableScanner
	groupSyncables := []*model.GroupSyncable{}

	sqlQuery := fmt.Sprintf("SELECT * from Group%[1]ss WHERE GroupId = :GroupId ORDER BY CreateAt DESC LIMIT :Limit OFFSET :Offset", syncableType.String())

	if _, err := s.GetReplica().Select(&groupSyncableScanners, sqlQuery, map[string]interface{}{"GroupId": groupID, "Limit": limit, "Offset": offset}); err != nil {
		if err == sql.ErrNoRows {
			result.Data = groupSyncables
			return result
		}
		result.Err = model.NewAppError("SqlGroupStore.GetAllGroupSyncablesByGroupPage", "store.sql_group.get_all_group_syncables_by_group_page.select_error", nil, err.Error(), http.StatusInternalServerError)
		return result
	}

	for _, gsScan := range groupSyncableScanners {
		gs := &model.GroupSyncable{
			GroupId:  gsScan.GroupId,
			CanLeave: gsScan.CanLeave,
			AutoAdd:  gsScan.AutoAdd,
			CreateAt: gsScan.CreateAt,
			UpdateAt: gsScan.UpdateAt,
			DeleteAt: gsScan.DeleteAt,
		}
		switch syncableType {
		case model.GSTeam:
			gs.SyncableId = gsScan.TeamId
		case model.GSChannel:
			gs.SyncableId = gsScan.ChannelId
		default:
			continue
		}
		groupSyncables = append(groupSyncables, gs)
	}

	result.Data = groupSyncables
	return result
}

func (s *SqlSupplier) GroupUpdateGroupSyncable(ctx context.Context, groupSyncable *model.GroupSyncable, hints ...store.LayeredStoreHint) *store.LayeredStoreSupplierResult {
	result := store.NewSupplierResult()

	retrievedGroupSyncable, err := s.getGroupSyncable(groupSyncable.GroupId, groupSyncable.SyncableId, groupSyncable.Type)
	if err != nil {
		if err == sql.ErrNoRows {
			result.Err = model.NewAppError("SqlGroupStore.UpdateGroupSyncable", "store.sql_group.update_group_syncable.no_rows", nil, err.Error(), http.StatusInternalServerError)
			return result
		}
		result.Err = model.NewAppError("SqlGroupStore.UpdateGroupSyncable", "store.sql_group.update_group_syncable.select_error", nil, "GroupId="+groupSyncable.GroupId+", SyncableId="+groupSyncable.SyncableId+", SyncableType="+groupSyncable.Type.String()+", "+err.Error(), http.StatusInternalServerError)
		return result
	}

	if err := groupSyncable.IsValid(); err != nil {
		result.Err = err
		return result
	}

	// Check if no update is required
	if (retrievedGroupSyncable.AutoAdd == groupSyncable.AutoAdd) && (retrievedGroupSyncable.CanLeave == groupSyncable.CanLeave) {
		result.Err = model.NewAppError("SqlGroupStore.UpdateGroupSyncable", "store.sql_group.update_group_syncable.no_change", nil, "group_id="+groupSyncable.GroupId+", syncable_id="+groupSyncable.SyncableId, http.StatusInternalServerError)
		return result
	}

	// Reset these properties, don't update them based on input
	groupSyncable.DeleteAt = retrievedGroupSyncable.DeleteAt
	groupSyncable.CreateAt = retrievedGroupSyncable.CreateAt
	groupSyncable.UpdateAt = model.GetMillis()

	var rowsAffected int64
	switch groupSyncable.Type {
	case model.GSTeam:
		rowsAffected, err = s.GetMaster().Update(&GroupTeam{
			*groupSyncable,
			groupSyncable.SyncableId,
		})
	case model.GSChannel:
		rowsAffected, err = s.GetMaster().Update(&GroupChannel{
			*groupSyncable,
			groupSyncable.SyncableId,
		})
	default:
		model.NewAppError("SqlGroupStore.CreateGroupSyncable", "store.sql_group.create_group_syncable.invalid_syncable_type", nil, "group_id="+groupSyncable.GroupId+", syncable_id="+groupSyncable.SyncableId+", "+err.Error(), http.StatusInternalServerError)
		return result
	}

	if err != nil {
		result.Err = model.NewAppError("SqlGroupStore.UpdateGroupSyncable", "store.sql_group.update_group_syncable.update_error", nil, err.Error(), http.StatusInternalServerError)
		return result
	}

	if rowsAffected == 0 {
		result.Err = model.NewAppError("SqlGroupStore.UpdateGroupSyncable", "store.sql_group.update_group_syncable.no_rows", nil, "GroupId="+groupSyncable.GroupId+", SyncableId="+groupSyncable.SyncableId+", SyncableType="+groupSyncable.Type.String()+", "+err.Error(), http.StatusInternalServerError)
		return result
	}

	result.Data = groupSyncable
	return result
}

func (s *SqlSupplier) GroupDeleteGroupSyncable(ctx context.Context, groupID string, syncableID string, syncableType model.GroupSyncableType, hints ...store.LayeredStoreHint) *store.LayeredStoreSupplierResult {
	result := store.NewSupplierResult()

	if !model.IsValidId(groupID) {
		result.Err = model.NewAppError("SqlGroupStore.DeleteGroupSyncable", "store.sql_group.delete_group_syncable.invalid_group_id", nil, "group_id="+groupID, http.StatusBadRequest)
		return result
	}

	if !model.IsValidId(string(syncableID)) {
		result.Err = model.NewAppError("SqlGroupStore.DeleteGroupSyncable", "store.sql_group.delete_group_syncable.invalid_syncable_id", nil, "group_id="+groupID, http.StatusBadRequest)
		return result
	}

	groupSyncable, err := s.getGroupSyncable(groupID, syncableID, syncableType)
	if err != nil {
		if err == sql.ErrNoRows {
			result.Err = model.NewAppError("SqlGroupStore.DeleteGroupSyncable", "store.sql_group.delete_group_syncable.no_rows", nil, "Id="+groupID+", "+err.Error(), http.StatusNotFound)
		} else {
			result.Err = model.NewAppError("SqlGroupStore.DeleteGroupSyncable", "store.sql_group.delete_group_syncable.select_error", nil, err.Error(), http.StatusInternalServerError)
		}
		return result
	}

	if groupSyncable.DeleteAt != 0 {
		result.Err = model.NewAppError("SqlGroupStore.DeleteGroupSyncable", "store.sql_group.delete_group_syncable.already_deleted", nil, "group_id="+groupID+"syncable_id="+syncableID, http.StatusBadRequest)
		return result
	}

	time := model.GetMillis()
	groupSyncable.DeleteAt = time
	groupSyncable.UpdateAt = time

	var rowsAffected int64
	switch groupSyncable.Type {
	case model.GSTeam:
		rowsAffected, err = s.GetMaster().Update(&GroupTeam{
			*groupSyncable,
			groupSyncable.SyncableId,
		})
	case model.GSChannel:
		rowsAffected, err = s.GetMaster().Update(&GroupChannel{
			*groupSyncable,
			groupSyncable.SyncableId,
		})
	default:
		model.NewAppError("SqlGroupStore.CreateGroupSyncable", "store.sql_group.create_group_syncable.invalid_syncable_type", nil, "group_id="+groupSyncable.GroupId+", syncable_id="+groupSyncable.SyncableId+", "+err.Error(), http.StatusInternalServerError)
		return result
	}

	if err != nil {
		result.Err = model.NewAppError("SqlGroupStore.DeleteGroupSyncable", "store.sql_group.delete_group_syncable.update_error", nil, err.Error(), http.StatusInternalServerError)
		return result
	}

	if rowsAffected == 0 {
		result.Err = model.NewAppError("SqlGroupStore.DeleteGroupSyncable", "store.sql_group.delete_group_syncable.no_rows_affected", nil, "", http.StatusInternalServerError)
		return result
	}

	result.Data = groupSyncable

	return result
}

// PendingAutoAddTeamMemberships returns a slice of [UserIds, TeamIds] tuples that need newly created
// memberships as configured by groups.
//
// Typically minGroupMembersCreateAt will be the last successful group sync time.
func (s *SqlSupplier) PendingAutoAddTeamMemberships(ctx context.Context, minGroupMembersCreateAt int, hints ...store.LayeredStoreHint) *store.LayeredStoreSupplierResult {
	result := store.NewSupplierResult()

	sql := `SELECT GroupMembers.UserId, GroupTeams.SyncableId
			FROM GroupMembers
			JOIN GroupTeams ON GroupTeams.GroupId = GroupMembers.GroupId
			JOIN Groups ON Groups.Id = GroupMembers.GroupId
			FULL JOIN TeamMembers ON TeamMembers.SyncableId = GroupTeams.SyncableId AND TeamMembers.UserId = GroupMembers.UserId
			WHERE TeamMembers.UserId IS NULL
			AND Groups.DeleteAt = 0
			AND GroupTeams.DeleteAt = 0
			AND GroupTeams.AutoAdd = true
			AND GroupMembers.DeleteAt = 0
			AND GroupMembers.CreateAt >= :MinGroupMembersCreateAt`

	sqlResult, err := s.GetMaster().Exec(sql, map[string]interface{}{"MinGroupMembersCreateAt": minGroupMembersCreateAt})
	if err != nil {
		result.Err = model.NewAppError("SqlGroupStore.PendingAutoAddTeamMemberships", "store.sql_group.select_error", nil, "", http.StatusInternalServerError)
	}

	result.Data = sqlResult

	return result
}

// PendingAutoAddChannelMemberships returns a slice [UserIds, ChannelIds] tuples that need newly created
// memberships as configured by groups.
//
// Typically minGroupMembersCreateAt will be the last successful group sync time.
func (s *SqlSupplier) PendingAutoAddChannelMemberships(minGroupMembersCreateAt int) *store.LayeredStoreSupplierResult {
	result := store.NewSupplierResult()

	sql := `SELECT GroupMembers.UserId, GroupChannels.ChannelId
			FROM GroupMembers
			JOIN GroupChannels ON GroupChannels.GroupId = GroupMembers.GroupId
			JOIN Groups ON Groups.Id = GroupMembers.GroupId
			JOIN Channels ON Channels.Id = GroupChannels.ChannelId
			JOIN Teams ON Teams.Id = Channels.SyncableId
			JOIN TeamMembers ON TeamMembers.SyncableId = Teams.Id AND TeamMembers.UserId = GroupMembers.UserId
			FULL JOIN ChannelMemberHistory ON ChannelMemberHistory.ChannelId = GroupChannels.ChannelId AND ChannelMemberHistory.UserId = GroupMembers.UserId
			WHERE ChannelMemberHistory.UserId IS NULL
			AND ChannelMemberHistory.LeaveTime IS NULL
			AND Groups.DeleteAt = 0
			AND GroupChannels.DeleteAt = 0
			AND GroupChannels.AutoAdd = true
			AND GroupMembers.DeleteAt = 0
			AND GroupMembers.CreateAt >= :MinGroupMembersCreateAt`

	sqlResult, err := s.GetMaster().Exec(sql, map[string]interface{}{"MinGroupMembersCreateAt": minGroupMembersCreateAt})
	if err != nil {
		result.Err = model.NewAppError("SqlGroupStore.PendingAutoAddChannelMemberships", "store.sql_group.select_error", nil, "", http.StatusInternalServerError)
	}

	result.Data = sqlResult

	return result
}
