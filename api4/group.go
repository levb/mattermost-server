// Copyright (c) 2018-present Mattermost, Inc. All Rights Reserved.
// See License.txt for license information.

package api4

import (
	"encoding/json"
	"net/http"

	"github.com/mattermost/mattermost-server/model"
)

const (
	apiGroupMemberActionCreate = iota
	apiGroupMemberActionDelete
)

func (api *API) InitGroup() {
	api.BaseRoutes.Groups.Handle("", api.ApiSessionRequired(createGroup)).Methods("POST")
	api.BaseRoutes.Groups.Handle("/{group_id:[A-Za-z0-9]+}", api.ApiSessionRequiredTrustRequester(getGroup)).Methods("GET")
	api.BaseRoutes.Groups.Handle("", api.ApiSessionRequired(getGroups)).Methods("GET")
	api.BaseRoutes.Groups.Handle("/{group_id:[A-Za-z0-9]+}", api.ApiSessionRequired(updateGroup)).Methods("PUT")
	api.BaseRoutes.Groups.Handle("/{group_id:[A-Za-z0-9]+}", api.ApiSessionRequired(deleteGroup)).Methods("DELETE")

	api.BaseRoutes.Groups.Handle("/{group_id:[A-Za-z0-9]+}/members", api.ApiSessionRequired(createOrDeleteGroupMember(apiGroupMemberActionCreate))).Methods("POST")
	api.BaseRoutes.Groups.Handle("/{group_id:[A-Za-z0-9]+}/members/{user_id:[A-Za-z0-9]+}", api.ApiSessionRequired(createOrDeleteGroupMember(apiGroupMemberActionDelete))).Methods("DELETE")

	api.BaseRoutes.Groups.Handle("/{group_id:[A-Za-z0-9]+}/teams", api.ApiSessionRequired(createGroupSyncable(model.GSTeam))).Methods("POST")
	api.BaseRoutes.Groups.Handle("/{group_id:[A-Za-z0-9]+}/teams", api.ApiSessionRequired(getGroupSyncables(model.GSTeam))).Methods("GET")
	api.BaseRoutes.Groups.Handle("/{group_id:[A-Za-z0-9]+}/teams/{team_id:[A-Za-z0-9]+}", api.ApiSessionRequired(getGroupSyncable(model.GSTeam))).Methods("GET")
	api.BaseRoutes.Groups.Handle("/{group_id:[A-Za-z0-9]+}/teams/{team_id:[A-Za-z0-9]+}", api.ApiSessionRequired(updateGroupSyncable(model.GSTeam))).Methods("PUT")
	api.BaseRoutes.Groups.Handle("/{group_id:[A-Za-z0-9]+}/teams/{team_id:[A-Za-z0-9]+}", api.ApiSessionRequired(deleteGroupSyncable(model.GSTeam))).Methods("DELETE")

	api.BaseRoutes.Groups.Handle("/{group_id:[A-Za-z0-9]+}/channels", api.ApiSessionRequired(createGroupSyncable(model.GSChannel))).Methods("POST")
	api.BaseRoutes.Groups.Handle("/{group_id:[A-Za-z0-9]+}/channels", api.ApiSessionRequired(getGroupSyncables(model.GSChannel))).Methods("GET")
	api.BaseRoutes.Groups.Handle("/{group_id:[A-Za-z0-9]+}/channels/{channel_id:[A-Za-z0-9]+}", api.ApiSessionRequired(getGroupSyncable(model.GSChannel))).Methods("GET")
	api.BaseRoutes.Groups.Handle("/{group_id:[A-Za-z0-9]+}/channels/{channel_id:[A-Za-z0-9]+}", api.ApiSessionRequired(updateGroupSyncable(model.GSChannel))).Methods("PUT")
	api.BaseRoutes.Groups.Handle("/{group_id:[A-Za-z0-9]+}/channels/{channel_id:[A-Za-z0-9]+}", api.ApiSessionRequired(deleteGroupSyncable(model.GSChannel))).Methods("DELETE")
}

func createGroup(c *Context, w http.ResponseWriter, r *http.Request) {
	group := model.GroupFromJson(r.Body)
	if group == nil {
		c.SetInvalidParam("group")
		return
	}

	if c.App.License() == nil || !*c.App.License().Features.LDAP {
		c.Err = model.NewAppError("Api4.CreateGroup", "api.group.create_group.license.error", nil, "", http.StatusNotImplemented)
		return
	}

	if !c.App.SessionHasPermissionTo(c.Session, model.PERMISSION_MANAGE_SYSTEM) {
		c.SetPermissionError(model.PERMISSION_MANAGE_SYSTEM)
		return
	}

	group, err := c.App.CreateGroup(group)
	if err != nil {
		c.Err = err
		return
	}

	w.WriteHeader(http.StatusCreated)

	b, _ := json.Marshal(group)

	w.Write(b)
}

func getGroup(c *Context, w http.ResponseWriter, r *http.Request) {
	c.RequireGroupId()
	if c.Err != nil {
		return
	}

	if !c.App.SessionHasPermissionTo(c.Session, model.PERMISSION_MANAGE_SYSTEM) {
		c.SetPermissionError(model.PERMISSION_MANAGE_SYSTEM)
		return
	}

	group, err := c.App.GetGroup(c.Params.GroupId)
	if err != nil {
		c.Err = err
		return
	}

	b, _ := json.Marshal(group)

	w.Write(b)
}

func getGroups(c *Context, w http.ResponseWriter, r *http.Request) {
	if !c.App.SessionHasPermissionTo(c.Session, model.PERMISSION_MANAGE_SYSTEM) {
		c.SetPermissionError(model.PERMISSION_MANAGE_SYSTEM)
		return
	}

	groups, err := c.App.GetGroupsPage(c.Params.Page, c.Params.PerPage)
	if err != nil {
		c.Err = err
		return
	}

	b, _ := json.Marshal(groups)

	w.Write(b)
}

func updateGroup(c *Context, w http.ResponseWriter, r *http.Request) {
	c.RequireGroupId()
	if c.Err != nil {
		return
	}

	update := model.GroupFromJson(r.Body)
	if update == nil {
		c.SetInvalidParam("group")
		return
	}

	if c.App.License() == nil || !*c.App.License().Features.LDAP {
		c.Err = model.NewAppError("Api4.UpdateGroup", "api.group.update_group.license.error", nil, "", http.StatusNotImplemented)
		return
	}

	if !c.App.SessionHasPermissionTo(c.Session, model.PERMISSION_MANAGE_SYSTEM) {
		c.SetPermissionError(model.PERMISSION_MANAGE_SYSTEM)
		return
	}

	update.Id = c.Params.GroupId

	group, err := c.App.UpdateGroup(update)
	if err != nil {
		c.Err = err
		return
	}

	// TODO: Is below necessary?
	// c.LogAudit("")

	b, _ := json.Marshal(group)

	w.Write(b)
}

func deleteGroup(c *Context, w http.ResponseWriter, r *http.Request) {
	c.RequireGroupId()
	if c.Err != nil {
		return
	}

	if c.App.License() == nil || !*c.App.License().Features.LDAP {
		c.Err = model.NewAppError("Api4.DeleteGroup", "api.group.delete_group.license.error", nil, "", http.StatusNotImplemented)
		return
	}

	if !c.App.SessionHasPermissionTo(c.Session, model.PERMISSION_MANAGE_SYSTEM) {
		c.SetPermissionError(model.PERMISSION_MANAGE_SYSTEM)
		return
	}

	if _, err := c.App.DeleteGroup(c.Params.GroupId); err != nil {
		c.Err = err
		return
	}

	ReturnStatusOK(w)
}

func createOrDeleteGroupMember(action int) func(*Context, http.ResponseWriter, *http.Request) {
	return func(c *Context, w http.ResponseWriter, r *http.Request) {
		c.RequireGroupId()
		if c.Err != nil {
			return
		}

		c.RequireUserId()
		if c.Err != nil {
			return
		}

		if c.App.License() == nil || !*c.App.License().Features.LDAP {
			c.Err = model.NewAppError("Api4.CreateGroupMember", "api.group.create_group_member.license.error", nil, "", http.StatusNotImplemented)
			return
		}

		if !c.App.SessionHasPermissionTo(c.Session, model.PERMISSION_MANAGE_SYSTEM) {
			c.SetPermissionError(model.PERMISSION_MANAGE_SYSTEM)
			return
		}

		var createOrDeleteF func(string, string) (*model.GroupMember, *model.AppError)
		var successStatus int
		switch action {
		case apiGroupMemberActionCreate:
			createOrDeleteF = c.App.CreateGroupMember
			successStatus = http.StatusCreated
		case apiGroupMemberActionDelete:
			createOrDeleteF = c.App.DeleteGroupMember
			successStatus = http.StatusOK
		default:
			return
		}

		groupMember, err := createOrDeleteF(c.Params.GroupId, c.Params.UserId)
		if err != nil {
			c.Err = err
			return
		}

		w.WriteHeader(successStatus)

		b, _ := json.Marshal(groupMember)

		w.Write(b)
	}
}

func createGroupSyncable(syncableType model.GroupSyncableType) func(*Context, http.ResponseWriter, *http.Request) {
	return func(c *Context, w http.ResponseWriter, r *http.Request) {

	}
}

func getGroupSyncables(syncableType model.GroupSyncableType) func(*Context, http.ResponseWriter, *http.Request) {
	return func(c *Context, w http.ResponseWriter, r *http.Request) {

	}
}

func getGroupSyncable(syncableType model.GroupSyncableType) func(*Context, http.ResponseWriter, *http.Request) {
	return func(c *Context, w http.ResponseWriter, r *http.Request) {

	}
}

func updateGroupSyncable(syncableType model.GroupSyncableType) func(*Context, http.ResponseWriter, *http.Request) {
	return func(c *Context, w http.ResponseWriter, r *http.Request) {

	}
}

func deleteGroupSyncable(syncableType model.GroupSyncableType) func(*Context, http.ResponseWriter, *http.Request) {
	return func(c *Context, w http.ResponseWriter, r *http.Request) {

	}
}
