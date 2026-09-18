package patchscenarios

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchtest"
)

var volumeAssertions = []string{
	"volume.create", "volume.readback", "volume.client-token", "volume.tags.readback",
	"volume.update.by-id", "volume.storage-role",
	"volume.filters", "volume.pagination", "volume.delete",
	"template.create", "template.readback", "template.client-token",
	"template.update", "template.update.by-id", "template.storage-role",
	"template.filters", "template.pagination", "template.delete",
	"mount.volume-ref", "mount.volume-ref.by-id", "mount.template-ref", "input.validation",
}

// AgentCBS is provisioned by the service itself, so the scenario can name a
// size without reserving anything in advance. The storage payload carries a
// GiB count; the volume reports the derived capacity string.
const (
	agentCBSCapacity       = "20Gi"
	agentCBSGrownCapacity  = "40Gi"
	agentCBSStoragePayload = `{"AgentCbs":{"Capacity":"20Gi"}}`
)

// volumeTagKey marks every resource the scenario creates, and doubles as the
// value the tag-key filter step selects on.
const volumeTagKey = "agr-e2e"

type volumeEnvelope struct {
	Status  string
	Data    map[string]any
	Failure *struct{ Code, Kind string }
}

// volumeStorage carries the reviewed backend identifiers. COS and CFS volumes
// bind real storage locations and mounts need a real tool role, so nothing here
// can be invented by the scenario.
type volumeStorage struct {
	cosEndpoint, cosBucket, cfsFileSystemID string
	storageRoleArn, toolType, toolRoleArn   string
}

func volumeStorageFromEnv() (volumeStorage, error) {
	fixture := volumeStorage{
		cosEndpoint:     os.Getenv("AGR_VOLUME_COS_ENDPOINT"),
		cosBucket:       os.Getenv("AGR_VOLUME_COS_BUCKET"),
		cfsFileSystemID: os.Getenv("AGR_VOLUME_CFS_FILESYSTEM_ID"),
		storageRoleArn:  os.Getenv("AGR_VOLUME_STORAGE_ROLE_ARN"),
		toolType:        os.Getenv("AGR_VOLUME_TOOL_TYPE"),
		toolRoleArn:     os.Getenv("AGR_VOLUME_TOOL_ROLE_ARN"),
	}
	for name, value := range map[string]string{
		"AGR_VOLUME_COS_ENDPOINT":      fixture.cosEndpoint,
		"AGR_VOLUME_COS_BUCKET":        fixture.cosBucket,
		"AGR_VOLUME_CFS_FILESYSTEM_ID": fixture.cfsFileSystemID,
		"AGR_VOLUME_STORAGE_ROLE_ARN":  fixture.storageRoleArn,
		"AGR_VOLUME_TOOL_TYPE":         fixture.toolType,
		"AGR_VOLUME_TOOL_ROLE_ARN":     fixture.toolRoleArn,
	} {
		if strings.TrimSpace(value) == "" {
			return volumeStorage{}, fmt.Errorf("%s must identify a reviewed disposable storage fixture", name)
		}
	}
	return fixture, nil
}

func volumeCLI(s *patchtest.Session, ctx context.Context, args ...string) (volumeEnvelope, error) {
	raw, callErr := s.CLI(ctx, append(args, "-o", "json")...)
	var out volumeEnvelope
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&out); err != nil {
		return out, fmt.Errorf("%s: invalid CLI envelope", strings.Join(args, " "))
	}
	if callErr != nil || out.Status != "succeeded" || out.Failure != nil {
		return out, fmt.Errorf("%s: CLI call failed", strings.Join(args, " "))
	}
	return out, nil
}

func volumeCall(s *patchtest.Session, ctx context.Context, command string, request map[string]any) (volumeEnvelope, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return volumeEnvelope{}, err
	}
	return volumeCLI(s, ctx, append(strings.Split(command, "."), "--request", string(payload))...)
}

// volumeQuery returns the rows and TotalCount together; both belong to the list
// contract, and an assertion that ignores TotalCount is not evidence.
func volumeQuery(s *patchtest.Session, ctx context.Context, command, setKey string, request map[string]any) ([]any, int64, error) {
	result, err := volumeCall(s, ctx, command, request)
	if err != nil {
		return nil, 0, err
	}
	return volumeSet(result, setKey)
}

func volumeSet(result volumeEnvelope, setKey string) ([]any, int64, error) {
	count, ok := result.Data["TotalCount"].(json.Number)
	if !ok {
		return nil, 0, errors.New("list response is missing TotalCount")
	}
	total, err := count.Int64()
	if err != nil {
		return nil, 0, err
	}
	rows, _ := result.Data[setKey].([]any)
	return rows, total, nil
}

// volumeJSONFile materializes a JSON flag value so the documented @file
// transport is exercised, not just inline JSON.
func volumeJSONFile(payload string) (string, func(), error) {
	file, err := os.CreateTemp("", "agr-volume-*.json")
	if err != nil {
		return "", nil, err
	}
	remove := func() { _ = os.Remove(file.Name()) }
	if _, err := file.WriteString(payload); err != nil {
		remove()
		return "", nil, err
	}
	if err := file.Close(); err != nil {
		remove()
		return "", nil, err
	}
	return "@" + file.Name(), remove, nil
}

func volumeObject(value any) map[string]any { object, _ := value.(map[string]any); return object }

func volumeString(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return value
}

func volumeNested(object map[string]any, path ...string) map[string]any {
	for _, key := range path {
		object = volumeObject(object[key])
	}
	return object
}

func volumeRow(rows []any, idKey, id string) map[string]any {
	for _, row := range rows {
		if object := volumeObject(row); volumeString(object, idKey) == id {
			return object
		}
	}
	return nil
}

// volumeTimestamps holds the readback to the declared datetime_iso format, so a
// service that returns a placeholder string cannot pass.
func volumeTimestamps(object map[string]any) bool {
	for _, key := range []string{"CreatedAt", "UpdatedAt"} {
		if _, err := time.Parse(time.RFC3339, volumeString(object, key)); err != nil {
			return false
		}
	}
	return true
}

// volumeAbsent treats an empty result set as the only acceptable proof of
// deletion. The API has no single-resource read, so the list query is the readback.
func volumeAbsent(s *patchtest.Session, ctx context.Context, command, setKey, idsField, id string) error {
	rows, total, err := volumeQuery(s, ctx, command, setKey, map[string]any{idsField: []string{id}})
	if err != nil {
		return err
	}
	if len(rows) != 0 || total != 0 {
		return errors.New("resource absence not confirmed")
	}
	return nil
}

// volumeOwned registers cleanup for one created resource immediately and returns
// the callback that marks it already deleted by the scenario body.
func volumeOwned(s *patchtest.Session, command, setKey, idsField, idField, id string) func() {
	deleted := false
	s.Cleanup(func(ctx context.Context) error {
		if !deleted {
			if _, err := volumeCall(s, ctx, command+".delete", map[string]any{idField: id}); err != nil {
				return err
			}
		}
		return volumeAbsent(s, ctx, command+".list", setKey, idsField, id)
	})
	return func() { deleted = true }
}

func volumeTags(pairs ...string) []any {
	tags := make([]any, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		tags = append(tags, map[string]any{"Key": pairs[i], "Value": pairs[i+1]})
	}
	return tags
}

func volumeTagsEqual(actual any, want []any) bool {
	rows, _ := actual.([]any)
	if len(rows) != len(want) {
		return false
	}
	for _, expected := range want {
		found := false
		for _, row := range rows {
			if reflect.DeepEqual(volumeObject(row), volumeObject(expected)) {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func runVolumeLifecycle(s *patchtest.Session) error {
	storage, err := volumeStorageFromEnv()
	if err != nil {
		return err
	}
	ctx := s.Context
	if err := volumeInputValidation(s, ctx, storage); err != nil {
		return err
	}
	name := fmt.Sprintf("agr-volume-e2e-%d", time.Now().UnixNano())
	templateName := name + "-tpl"
	volumeName := name + "-vol"

	templateID, templateDeleted, err := volumeTemplateLifecycle(s, ctx, storage, templateName)
	if err != nil {
		return err
	}
	volumeID, volumeDeleted, err := volumeResourceLifecycle(s, ctx, storage, volumeName)
	if err != nil {
		return err
	}
	if err := volumeMountReferences(s, ctx, storage, name, volumeName, volumeID, templateName); err != nil {
		return err
	}

	if _, err := volumeCall(s, ctx, "volume.delete", map[string]any{"VolumeId": volumeID}); err != nil {
		return err
	}
	volumeDeleted()
	if err := volumeAbsent(s, ctx, "volume.list", "VolumeSet", "VolumeIds", volumeID); err != nil {
		return err
	}
	if err := s.Assert("volume.delete", true); err != nil {
		return err
	}
	if _, err := volumeCall(s, ctx, "volume-template.delete", map[string]any{"VolumeTemplateId": templateID}); err != nil {
		return err
	}
	templateDeleted()
	if err := volumeAbsent(s, ctx, "volume-template.list", "VolumeTemplateSet", "VolumeTemplateIds", templateID); err != nil {
		return err
	}
	// The cleanup callbacks independently confirm every resource is absent.
	return s.Assert("template.delete", true)
}

// runVolumeAgentCbs isolates the AgentCBS backend. It is a separate scenario so
// that a defect in one storage backend cannot mask the rest of the contract.
func runVolumeAgentCbs(s *patchtest.Session) error {
	if _, err := volumeStorageFromEnv(); err != nil {
		return err
	}
	ctx := s.Context
	name := fmt.Sprintf("agr-volume-cbs-e2e-%d", time.Now().UnixNano())
	if err := volumeTemplateCbs(s, ctx, name+"-tpl"); err != nil {
		return err
	}
	return volumeCbs(s, ctx, name+"-vol")
}

// volumeInputValidation pins the usage contract before any resource exists, so a
// rejected request can never be mistaken for a backend failure.
func volumeInputValidation(s *patchtest.Session, ctx context.Context, storage volumeStorage) error {
	for _, invalid := range []struct {
		command string
		request map[string]any
	}{
		{"volume.create", map[string]any{"VolumeName": "agr-volume-validation"}},
		{"volume.create", map[string]any{
			"VolumeName": "agr-volume-validation", "AccessMode": "ReadWriteMany", "StorageType": "Cfs",
			"Storage": map[string]any{"Cfs": map[string]any{"FileSystemId": storage.cfsFileSystemID, "Path": "validation"}},
			// SourceVolumeId is disabled in the CLI contract and must not reach the service.
			"SourceVolumeId": "vol-validation",
		}},
		{"volume.list", map[string]any{"Limit": "invalid"}},
		{"volume.list", map[string]any{"Filters": map[string]any{"Name": "status"}}},
		{"volume-template.create", map[string]any{"VolumeTemplateName": "agr-volume-validation"}},
		{"volume-template.update", map[string]any{"VolumeTemplateId": "volt-validation", "Tags": "invalid"}},
	} {
		result, err := volumeCall(s, ctx, invalid.command, invalid.request)
		if err := s.Assert("input.validation", err != nil && result.Failure != nil && result.Failure.Kind == "usage"); err != nil {
			return err
		}
	}
	return nil
}

func volumeTemplateLifecycle(s *patchtest.Session, ctx context.Context, storage volumeStorage, templateName string) (string, func(), error) {
	tags := volumeTags(volumeTagKey, "volume-template")
	pathPattern := "agr-e2e/" + templateName + "/${reuse_key}"
	request := map[string]any{
		"VolumeTemplateName": templateName,
		"AccessMode":         "ReadWriteMany",
		"ReclaimPolicy":      "Delete",
		"StorageType":        "Cos",
		"StorageSpec": map[string]any{"Cos": map[string]any{
			"Endpoint":          storage.cosEndpoint,
			"BucketName":        storage.cosBucket,
			"BucketPathPattern": pathPattern,
		}},
		"VolumeNamePattern": templateName + "-${reuse_key}",
		"StorageRoleArn":    storage.storageRoleArn,
		"Tags":              tags,
		"ClientToken":       templateName + "-token",
	}
	created, err := volumeCall(s, ctx, "volume-template.create", request)
	template := volumeObject(created.Data["VolumeTemplate"])
	templateID := volumeString(template, "VolumeTemplateId")
	markDeleted := func() {}
	if templateID != "" {
		markDeleted = volumeOwned(s, "volume-template", "VolumeTemplateSet", "VolumeTemplateIds", "VolumeTemplateId", templateID)
	}
	if err != nil {
		return "", nil, err
	}
	spec := volumeNested(template, "StorageSpec", "Cos")
	if err := s.Assert("template.create", templateID != "" &&
		volumeString(template, "VolumeTemplateName") == templateName &&
		volumeString(template, "AccessMode") == "ReadWriteMany" &&
		volumeString(template, "ReclaimPolicy") == "Delete" &&
		volumeString(template, "StorageType") == "Cos" &&
		volumeString(template, "VolumeNamePattern") == templateName+"-${reuse_key}" &&
		volumeString(spec, "Endpoint") == storage.cosEndpoint &&
		volumeString(spec, "BucketName") == storage.cosBucket &&
		volumeString(spec, "BucketPathPattern") == pathPattern); err != nil {
		return "", nil, err
	}

	repeat, err := volumeCall(s, ctx, "volume-template.create", request)
	repeatID := volumeString(volumeObject(repeat.Data["VolumeTemplate"]), "VolumeTemplateId")
	// A replayed token must return the first template. If the service minted a
	// second one instead, the scenario owns it and has to clean it up.
	if repeatID != "" && repeatID != templateID {
		volumeOwned(s, "volume-template", "VolumeTemplateSet", "VolumeTemplateIds", "VolumeTemplateId", repeatID)
	}
	if err != nil {
		return "", nil, err
	}
	if err := s.Assert("template.client-token", repeatID == templateID); err != nil {
		return "", nil, err
	}

	rows, total, err := volumeQuery(s, ctx, "volume-template.list", "VolumeTemplateSet", map[string]any{"VolumeTemplateIds": []string{templateID}})
	if err != nil {
		return "", nil, err
	}
	stored := volumeRow(rows, "VolumeTemplateId", templateID)
	if err := s.Assert("template.readback", total == 1 && len(rows) == 1 && stored != nil &&
		volumeString(stored, "Status") != "" && volumeTimestamps(stored) &&
		volumeString(volumeNested(stored, "StorageSpec", "Cos"), "BucketPathPattern") == pathPattern &&
		volumeTagsEqual(stored["Tags"], tags)); err != nil {
		return "", nil, err
	}
	if err := s.Assert("template.storage-role", volumeString(stored, "StorageRoleArn") == storage.storageRoleArn); err != nil {
		return "", nil, err
	}

	updated := volumeTags(volumeTagKey, "volume-template", "stage", "updated")
	if _, err := volumeCall(s, ctx, "volume-template.update", map[string]any{
		"VolumeTemplateName": templateName, "Tags": updated,
	}); err != nil {
		return "", nil, err
	}
	rows, _, err = volumeQuery(s, ctx, "volume-template.list", "VolumeTemplateSet", map[string]any{"VolumeTemplateIds": []string{templateID}})
	if err != nil {
		return "", nil, err
	}
	stored = volumeRow(rows, "VolumeTemplateId", templateID)
	if err := s.Assert("template.update", stored != nil && volumeTagsEqual(stored["Tags"], updated)); err != nil {
		return "", nil, err
	}

	// The ID selector must reach the same resource as the name selector.
	byID := volumeTags(volumeTagKey, "volume-template", "stage", "by-id")
	if _, err := volumeCall(s, ctx, "volume-template.update", map[string]any{
		"VolumeTemplateId": templateID, "Tags": byID,
	}); err != nil {
		return "", nil, err
	}
	rows, _, err = volumeQuery(s, ctx, "volume-template.list", "VolumeTemplateSet", map[string]any{"VolumeTemplateIds": []string{templateID}})
	if err != nil {
		return "", nil, err
	}
	stored = volumeRow(rows, "VolumeTemplateId", templateID)
	if err := s.Assert("template.update.by-id", stored != nil && volumeTagsEqual(stored["Tags"], byID)); err != nil {
		return "", nil, err
	}

	secondID, secondDeleted, err := volumeTemplateSecond(s, ctx, storage, templateName+"-page")
	if err != nil {
		return "", nil, err
	}
	if err := volumeTemplateFilters(s, ctx, templateID, secondID, templateName); err != nil {
		return "", nil, err
	}
	if err := volumePaginate(s, ctx, "volume-template.list", "VolumeTemplateSet", "VolumeTemplateIds", "VolumeTemplateId",
		"template.pagination", []string{templateID, secondID}); err != nil {
		return "", nil, err
	}

	// Deleting by name proves the name selector, and leaves the ID selector for
	// the final delete in the caller.
	if _, err := volumeCall(s, ctx, "volume-template.delete", map[string]any{"VolumeTemplateName": templateName + "-page"}); err != nil {
		return "", nil, err
	}
	secondDeleted()
	if err := volumeAbsent(s, ctx, "volume-template.list", "VolumeTemplateSet", "VolumeTemplateIds", secondID); err != nil {
		return "", nil, err
	}
	if err := s.Assert("template.delete", true); err != nil {
		return "", nil, err
	}
	return templateID, markDeleted, nil
}

// volumeTemplateSecond creates the CFS-backed counterpart that the filter and
// pagination steps need alongside the COS template.
func volumeTemplateSecond(s *patchtest.Session, ctx context.Context, storage volumeStorage, name string) (string, func(), error) {
	created, err := volumeCall(s, ctx, "volume-template.create", map[string]any{
		"VolumeTemplateName": name,
		"AccessMode":         "ReadOnlyMany",
		"ReclaimPolicy":      "Retain",
		"StorageType":        "Cfs",
		"StorageSpec": map[string]any{"Cfs": map[string]any{
			"FileSystemId": storage.cfsFileSystemID,
			"PathPattern":  "agr-e2e/" + name + "/${reuse_key}",
		}},
		"StorageRoleArn": storage.storageRoleArn,
	})
	template := volumeObject(created.Data["VolumeTemplate"])
	id := volumeString(template, "VolumeTemplateId")
	markDeleted := func() {}
	if id != "" {
		markDeleted = volumeOwned(s, "volume-template", "VolumeTemplateSet", "VolumeTemplateIds", "VolumeTemplateId", id)
	}
	if err != nil {
		return "", nil, err
	}
	spec := volumeNested(template, "StorageSpec", "Cfs")
	if err := s.Assert("template.create", id != "" &&
		volumeString(template, "AccessMode") == "ReadOnlyMany" &&
		volumeString(template, "ReclaimPolicy") == "Retain" &&
		volumeString(template, "StorageType") == "Cfs" &&
		volumeString(spec, "FileSystemId") == storage.cfsFileSystemID &&
		volumeString(spec, "PathPattern") == "agr-e2e/"+name+"/${reuse_key}"); err != nil {
		return "", nil, err
	}
	return id, markDeleted, nil
}

// volumeTemplateCbs covers the service-provisioned backend, where capacity is
// the only spec input and the update selector is the template ID. It drives the
// individual flags rather than --request, so the generated flag surface is
// covered as well.
func volumeTemplateCbs(s *patchtest.Session, ctx context.Context, name string) error {
	created, err := volumeCLI(s, ctx, "volume-template", "create",
		"--volume-template-name", name,
		"--access-mode", "ReadWriteOnce",
		"--reclaim-policy", "Delete",
		"--storage-type", "AgentCbs",
		"--storage-spec", agentCBSStoragePayload,
		"--default-capacity", agentCBSCapacity,
	)
	template := volumeObject(created.Data["VolumeTemplate"])
	id := volumeString(template, "VolumeTemplateId")
	markDeleted := func() {}
	if id != "" {
		markDeleted = volumeOwned(s, "volume-template", "VolumeTemplateSet", "VolumeTemplateIds", "VolumeTemplateId", id)
	}
	if err != nil {
		return err
	}
	if err := s.Assert("template.agent-cbs", id != "" &&
		volumeString(template, "StorageType") == "AgentCBS" &&
		volumeString(template, "DefaultCapacity") == agentCBSCapacity &&
		volumeString(volumeNested(template, "StorageSpec", "AgentCBS"), "Capacity") == agentCBSCapacity); err != nil {
		return err
	}

	grown := agentCBSGrownCapacity
	tags := volumeTags(volumeTagKey, "volume-template-cbs")
	encoded, err := json.Marshal(tags)
	if err != nil {
		return err
	}
	if _, err := volumeCLI(s, ctx, "volume-template", "update",
		"--volume-template-id", id, "--default-capacity", grown, "--tags", string(encoded)); err != nil {
		return err
	}
	listed, err := volumeCLI(s, ctx, "volume-template", "list", "--volume-template-ids", id)
	if err != nil {
		return err
	}
	rows, total, err := volumeSet(listed, "VolumeTemplateSet")
	if err != nil {
		return err
	}
	stored := volumeRow(rows, "VolumeTemplateId", id)
	if err := s.Assert("template.agent-cbs", total == 1 && stored != nil &&
		volumeString(stored, "DefaultCapacity") == grown &&
		volumeTagsEqual(stored["Tags"], tags)); err != nil {
		return err
	}

	if _, err := volumeCLI(s, ctx, "volume-template", "delete", "--volume-template-id", id); err != nil {
		return err
	}
	markDeleted()
	if err := volumeAbsent(s, ctx, "volume-template.list", "VolumeTemplateSet", "VolumeTemplateIds", id); err != nil {
		return err
	}
	return s.Assert("template.agent-cbs", true)
}

func volumeTemplateFilters(s *patchtest.Session, ctx context.Context, templateID, secondID, templateName string) error {
	both := []string{templateID, secondID}
	for _, tc := range []struct {
		request map[string]any
		want    int64
	}{
		{map[string]any{"VolumeTemplateIds": []string{templateID}}, 1},
		{map[string]any{"VolumeTemplateIds": both}, 2},
		{map[string]any{"VolumeTemplateNames": []string{templateName}}, 1},
		{map[string]any{"VolumeTemplateNames": []string{templateName + "-absent"}}, 0},
		{map[string]any{"VolumeTemplateIds": []string{"volt-absent"}}, 0},
		{map[string]any{"VolumeTemplateIds": both, "Filters": []any{
			map[string]any{"Name": "volume-template-name", "Values": []string{templateName}},
		}}, 1},
		// Values inside one filter are ORed.
		{map[string]any{"VolumeTemplateIds": both, "Filters": []any{
			map[string]any{"Name": "volume-template-name", "Values": []string{templateName, templateName + "-page"}},
		}}, 2},
		// Only the first template carries tags, so a second dimension narrows
		// the same pair back to one row.
		{map[string]any{"VolumeTemplateIds": both, "Filters": []any{
			map[string]any{"Name": "tag-key", "Values": []string{volumeTagKey}},
		}}, 1},
		// Separate filters are ANDed.
		{map[string]any{"VolumeTemplateIds": both, "Filters": []any{
			map[string]any{"Name": "volume-template-name", "Values": []string{templateName, templateName + "-page"}},
			map[string]any{"Name": "tag-key", "Values": []string{volumeTagKey}},
		}}, 1},
	} {
		rows, total, err := volumeQuery(s, ctx, "volume-template.list", "VolumeTemplateSet", tc.request)
		if err != nil {
			return err
		}
		if err := s.Assert("template.filters", total == tc.want && int64(len(rows)) == tc.want); err != nil {
			return err
		}
	}
	return nil
}

func volumeResourceLifecycle(s *patchtest.Session, ctx context.Context, storage volumeStorage, volumeName string) (string, func(), error) {
	tags := volumeTags(volumeTagKey, "volume")
	path := "agr-e2e/" + volumeName
	request := map[string]any{
		"VolumeName":  volumeName,
		"AccessMode":  "ReadWriteMany",
		"StorageType": "Cfs",
		"Storage": map[string]any{"Cfs": map[string]any{
			"FileSystemId": storage.cfsFileSystemID,
			"Path":         path,
		}},
		"Tags":        tags,
		"ClientToken": volumeName + "-token",
	}
	created, err := volumeCall(s, ctx, "volume.create", request)
	volume := volumeObject(created.Data["Volume"])
	volumeID := volumeString(volume, "VolumeId")
	markDeleted := func() {}
	if volumeID != "" {
		markDeleted = volumeOwned(s, "volume", "VolumeSet", "VolumeIds", "VolumeId", volumeID)
	}
	if err != nil {
		return "", nil, err
	}
	source := volumeNested(volume, "Storage", "Cfs")
	if err := s.Assert("volume.create", volumeID != "" &&
		volumeString(volume, "VolumeName") == volumeName &&
		volumeString(volume, "AccessMode") == "ReadWriteMany" &&
		volumeString(volume, "StorageType") == "Cfs" &&
		volumeString(source, "FileSystemId") == storage.cfsFileSystemID &&
		volumeString(source, "Path") == path); err != nil {
		return "", nil, err
	}

	repeat, err := volumeCall(s, ctx, "volume.create", request)
	repeatID := volumeString(volumeObject(repeat.Data["Volume"]), "VolumeId")
	if repeatID != "" && repeatID != volumeID {
		volumeOwned(s, "volume", "VolumeSet", "VolumeIds", "VolumeId", repeatID)
	}
	if err != nil {
		return "", nil, err
	}
	if err := s.Assert("volume.client-token", repeatID == volumeID); err != nil {
		return "", nil, err
	}

	rows, total, err := volumeQuery(s, ctx, "volume.list", "VolumeSet", map[string]any{"VolumeIds": []string{volumeID}})
	if err != nil {
		return "", nil, err
	}
	stored := volumeRow(rows, "VolumeId", volumeID)
	if err := s.Assert("volume.readback", total == 1 && len(rows) == 1 && stored != nil &&
		volumeString(stored, "Status") != "" && volumeTimestamps(stored) &&
		slices.Contains([]string{"Retain", "Delete"}, volumeString(stored, "ReclaimPolicy")) &&
		// A standalone, unmounted CFS volume carries no derivation, no mount and
		// no capacity of its own.
		volumeString(stored, "VolumeTemplateId") == "" && volumeString(stored, "ReuseKey") == "" &&
		volumeString(stored, "MountedInstanceId") == "" && volumeString(stored, "Capacity") == "" &&
		volumeString(volumeNested(stored, "Storage", "Cfs"), "Path") == path &&
		volumeTagsEqual(stored["Tags"], tags)); err != nil {
		return "", nil, err
	}

	updated := volumeTags(volumeTagKey, "volume", "stage", "updated")
	if _, err := volumeCall(s, ctx, "volume.update", map[string]any{"VolumeName": volumeName, "Tags": updated}); err != nil {
		return "", nil, err
	}
	rows, _, err = volumeQuery(s, ctx, "volume.list", "VolumeSet", map[string]any{"VolumeNames": []string{volumeName}})
	if err != nil {
		return "", nil, err
	}
	stored = volumeRow(rows, "VolumeId", volumeID)
	if err := s.Assert("volume.tags.readback", stored != nil && volumeTagsEqual(stored["Tags"], updated)); err != nil {
		return "", nil, err
	}

	// The ID selector must reach the same resource as the name selector.
	byID := volumeTags(volumeTagKey, "volume", "stage", "by-id")
	if _, err := volumeCall(s, ctx, "volume.update", map[string]any{"VolumeId": volumeID, "Tags": byID}); err != nil {
		return "", nil, err
	}
	rows, _, err = volumeQuery(s, ctx, "volume.list", "VolumeSet", map[string]any{"VolumeIds": []string{volumeID}})
	if err != nil {
		return "", nil, err
	}
	stored = volumeRow(rows, "VolumeId", volumeID)
	if err := s.Assert("volume.update.by-id", stored != nil && volumeTagsEqual(stored["Tags"], byID)); err != nil {
		return "", nil, err
	}

	secondID, secondDeleted, err := volumeSecond(s, ctx, storage, volumeName+"-page")
	if err != nil {
		return "", nil, err
	}
	if err := volumeFilters(s, ctx, volumeID, secondID, volumeName); err != nil {
		return "", nil, err
	}
	if err := volumePaginate(s, ctx, "volume.list", "VolumeSet", "VolumeIds", "VolumeId",
		"volume.pagination", []string{volumeID, secondID}); err != nil {
		return "", nil, err
	}

	if _, err := volumeCall(s, ctx, "volume.delete", map[string]any{"VolumeName": volumeName + "-page"}); err != nil {
		return "", nil, err
	}
	secondDeleted()
	if err := volumeAbsent(s, ctx, "volume.list", "VolumeSet", "VolumeIds", secondID); err != nil {
		return "", nil, err
	}
	if err := s.Assert("volume.delete", true); err != nil {
		return "", nil, err
	}
	return volumeID, markDeleted, nil
}

// volumeSecond creates the COS-backed counterpart. It also carries the storage
// role, which only makes sense on a volume that reaches an external bucket.
func volumeSecond(s *patchtest.Session, ctx context.Context, storage volumeStorage, name string) (string, func(), error) {
	created, err := volumeCall(s, ctx, "volume.create", map[string]any{
		"VolumeName":  name,
		"AccessMode":  "ReadOnlyMany",
		"StorageType": "Cos",
		"Storage": map[string]any{"Cos": map[string]any{
			"Endpoint":   storage.cosEndpoint,
			"BucketName": storage.cosBucket,
			"BucketPath": "agr-e2e/" + name,
		}},
		"StorageRoleArn": storage.storageRoleArn,
	})
	volume := volumeObject(created.Data["Volume"])
	id := volumeString(volume, "VolumeId")
	markDeleted := func() {}
	if id != "" {
		markDeleted = volumeOwned(s, "volume", "VolumeSet", "VolumeIds", "VolumeId", id)
	}
	if err != nil {
		return "", nil, err
	}
	source := volumeNested(volume, "Storage", "Cos")
	if err := s.Assert("volume.create", id != "" &&
		volumeString(volume, "AccessMode") == "ReadOnlyMany" &&
		volumeString(volume, "StorageType") == "Cos" &&
		volumeString(source, "Endpoint") == storage.cosEndpoint &&
		volumeString(source, "BucketName") == storage.cosBucket &&
		volumeString(source, "BucketPath") == "agr-e2e/"+name); err != nil {
		return "", nil, err
	}
	rows, _, err := volumeQuery(s, ctx, "volume.list", "VolumeSet", map[string]any{"VolumeIds": []string{id}})
	if err != nil {
		return "", nil, err
	}
	stored := volumeRow(rows, "VolumeId", id)
	if err := s.Assert("volume.storage-role", stored != nil &&
		volumeString(stored, "StorageRoleArn") == storage.storageRoleArn); err != nil {
		return "", nil, err
	}
	return id, markDeleted, nil
}

// volumeCbs covers the service-provisioned backend, where the reported capacity
// is the evidence, and updates by volume ID rather than by name. The storage
// payload arrives through @file, which the flag help promises.
func volumeCbs(s *patchtest.Session, ctx context.Context, name string) error {
	storageFlag, removeFile, err := volumeJSONFile(agentCBSStoragePayload)
	if err != nil {
		return err
	}
	defer removeFile()
	created, err := volumeCLI(s, ctx, "volume", "create",
		"--volume-name", name,
		"--access-mode", "ReadWriteOnce",
		"--storage-type", "AgentCbs",
		"--storage", storageFlag,
	)
	volume := volumeObject(created.Data["Volume"])
	id := volumeString(volume, "VolumeId")
	markDeleted := func() {}
	if id != "" {
		markDeleted = volumeOwned(s, "volume", "VolumeSet", "VolumeIds", "VolumeId", id)
	}
	if err != nil {
		return err
	}
	if err := s.Assert("volume.agent-cbs", id != "" &&
		volumeString(volume, "StorageType") == "AgentCBS" &&
		volumeString(volumeNested(volume, "Storage", "AgentCBS"), "Capacity") == agentCBSCapacity); err != nil {
		return err
	}
	listed, err := volumeCLI(s, ctx, "volume", "list", "--volume-ids", id)
	if err != nil {
		return err
	}
	rows, total, err := volumeSet(listed, "VolumeSet")
	if err != nil {
		return err
	}
	stored := volumeRow(rows, "VolumeId", id)
	if err := s.Assert("volume.agent-cbs", total == 1 && stored != nil &&
		volumeString(stored, "Capacity") == agentCBSCapacity); err != nil {
		return err
	}

	tags := volumeTags(volumeTagKey, "volume-cbs")
	encoded, err := json.Marshal(tags)
	if err != nil {
		return err
	}
	if _, err := volumeCLI(s, ctx, "volume", "update", "--volume-id", id, "--tags", string(encoded)); err != nil {
		return err
	}
	listed, err = volumeCLI(s, ctx, "volume", "list", "--volume-ids", id)
	if err != nil {
		return err
	}
	rows, _, err = volumeSet(listed, "VolumeSet")
	if err != nil {
		return err
	}
	stored = volumeRow(rows, "VolumeId", id)
	if err := s.Assert("volume.agent-cbs", stored != nil && volumeTagsEqual(stored["Tags"], tags)); err != nil {
		return err
	}

	if _, err := volumeCLI(s, ctx, "volume", "delete", "--volume-id", id); err != nil {
		return err
	}
	markDeleted()
	if err := volumeAbsent(s, ctx, "volume.list", "VolumeSet", "VolumeIds", id); err != nil {
		return err
	}
	return s.Assert("volume.agent-cbs", true)
}

func volumeFilters(s *patchtest.Session, ctx context.Context, volumeID, secondID, volumeName string) error {
	both := []string{volumeID, secondID}
	for _, tc := range []struct {
		request map[string]any
		want    int64
	}{
		{map[string]any{"VolumeIds": []string{volumeID}}, 1},
		{map[string]any{"VolumeIds": both}, 2},
		{map[string]any{"VolumeNames": []string{volumeName}}, 1},
		{map[string]any{"VolumeNames": []string{volumeName + "-absent"}}, 0},
		{map[string]any{"VolumeIds": []string{"vol-absent"}}, 0},
		{map[string]any{"VolumeIds": both, "Filters": []any{
			map[string]any{"Name": "volume-name", "Values": []string{volumeName}},
		}}, 1},
		// Values inside one filter are ORed.
		{map[string]any{"VolumeIds": both, "Filters": []any{
			map[string]any{"Name": "volume-name", "Values": []string{volumeName, volumeName + "-page"}},
		}}, 2},
		// Only the first volume carries tags, so a second dimension narrows the
		// same pair back to one row.
		{map[string]any{"VolumeIds": both, "Filters": []any{
			map[string]any{"Name": "tag-key", "Values": []string{volumeTagKey}},
		}}, 1},
		// Separate filters are ANDed.
		{map[string]any{"VolumeIds": both, "Filters": []any{
			map[string]any{"Name": "volume-name", "Values": []string{volumeName, volumeName + "-page"}},
			map[string]any{"Name": "tag-key", "Values": []string{volumeTagKey}},
		}}, 1},
	} {
		rows, total, err := volumeQuery(s, ctx, "volume.list", "VolumeSet", tc.request)
		if err != nil {
			return err
		}
		if err := s.Assert("volume.filters", total == tc.want && int64(len(rows)) == tc.want); err != nil {
			return err
		}
	}
	return nil
}

// volumePaginate walks the owned resources one page at a time, so a service that
// ignores Offset or Limit cannot produce a passing run.
func volumePaginate(s *patchtest.Session, ctx context.Context, command, setKey, idsField, idField, assertion string, ids []string) error {
	seen := map[string]bool{}
	for offset := range ids {
		rows, total, err := volumeQuery(s, ctx, command, setKey, map[string]any{
			idsField: ids, "Offset": offset, "Limit": 1,
		})
		if err != nil {
			return err
		}
		if err := s.Assert(assertion, total == int64(len(ids)) && len(rows) == 1); err != nil {
			return err
		}
		id := volumeString(volumeObject(rows[0]), idField)
		if err := s.Assert(assertion, id != "" && !seen[id]); err != nil {
			return err
		}
		seen[id] = true
	}
	for _, id := range ids {
		if err := s.Assert(assertion, seen[id]); err != nil {
			return err
		}
	}
	return nil
}

// volumeMountReferences proves the StorageMount additions survive the round trip.
// The CLI contract only promises that the references are sent and read back;
// derivation happens when an instance starts and is not asserted here.
func volumeMountReferences(s *patchtest.Session, ctx context.Context, storage volumeStorage, name, volumeName, volumeID, templateName string) error {
	reuseKey := "${metadata.session_id}"
	created, err := volumeCall(s, ctx, "tool.create", map[string]any{
		"ToolName":             name + "-tool",
		"ToolType":             storage.toolType,
		"RoleArn":              storage.toolRoleArn,
		"NetworkConfiguration": map[string]any{"NetworkMode": "PUBLIC"},
		"StorageMounts": []any{
			map[string]any{
				"Name": "volume-by-name", "MountPath": "/mnt/by-name", "ReadOnly": false,
				"Volume": map[string]any{"VolumeName": volumeName},
			},
			map[string]any{
				"Name": "volume-by-id", "MountPath": "/mnt/by-id", "ReadOnly": true,
				"Volume": map[string]any{"VolumeId": volumeID},
			},
			map[string]any{
				"Name": "derived-volume", "MountPath": "/mnt/session", "ReadOnly": true,
				"VolumeTemplate": map[string]any{"VolumeTemplateName": templateName, "ReuseKey": reuseKey},
			},
		},
	})
	toolID := volumeString(created.Data, "ToolId")
	toolDeleted := false
	if toolID != "" {
		// The tool exists only to carry the mount references, so cleanup removes it
		// through the raw action and proves absence with the list command.
		s.Cleanup(func(ctx context.Context) error {
			if !toolDeleted {
				if err := volumeDeleteTool(s, ctx, toolID); err != nil {
					return err
				}
			}
			remaining, err := volumeCall(s, ctx, "tool.list", map[string]any{"ToolIds": []string{toolID}})
			if err != nil {
				return err
			}
			if rows, _ := remaining.Data["Items"].([]any); len(rows) != 0 {
				return errors.New("tool absence not confirmed")
			}
			return nil
		})
	}
	if err != nil {
		return err
	}
	if toolID == "" {
		return errors.New("tool create returned no ToolId")
	}
	result, err := volumeCall(s, ctx, "tool.list", map[string]any{"ToolIds": []string{toolID}})
	if err != nil {
		return err
	}
	items, _ := result.Data["Items"].([]any)
	stored := volumeRow(items, "ToolId", toolID)
	if stored == nil {
		return errors.New("tool readback missing")
	}
	mounts := map[string]map[string]any{}
	rows, _ := stored["StorageMounts"].([]any)
	for _, row := range rows {
		mount := volumeObject(row)
		mounts[volumeString(mount, "Name")] = mount
	}
	byName := mounts["volume-by-name"]
	if err := s.Assert("mount.volume-ref", byName != nil &&
		volumeString(byName, "MountPath") == "/mnt/by-name" &&
		volumeString(volumeObject(byName["Volume"]), "VolumeName") == volumeName &&
		byName["VolumeTemplate"] == nil && byName["StorageSource"] == nil); err != nil {
		return err
	}
	byID := mounts["volume-by-id"]
	if err := s.Assert("mount.volume-ref.by-id", byID != nil &&
		volumeString(byID, "MountPath") == "/mnt/by-id" &&
		volumeString(volumeObject(byID["Volume"]), "VolumeId") == volumeID &&
		byID["VolumeTemplate"] == nil && byID["StorageSource"] == nil); err != nil {
		return err
	}
	derived := mounts["derived-volume"]
	if err := s.Assert("mount.template-ref", derived != nil &&
		volumeString(derived, "MountPath") == "/mnt/session" &&
		volumeString(volumeObject(derived["VolumeTemplate"]), "VolumeTemplateName") == templateName &&
		volumeString(volumeObject(derived["VolumeTemplate"]), "ReuseKey") == reuseKey &&
		derived["Volume"] == nil && derived["StorageSource"] == nil); err != nil {
		return err
	}
	// The referenced volume and template are deleted next, so the tool that
	// references them must go first.
	if err := volumeDeleteTool(s, ctx, toolID); err != nil {
		return err
	}
	toolDeleted = true
	return nil
}

func volumeDeleteTool(s *patchtest.Session, ctx context.Context, toolID string) error {
	payload, err := json.Marshal(map[string]any{"ToolId": toolID})
	if err != nil {
		return err
	}
	_, err = volumeCLI(s, ctx, "api", "call", "DeleteSandboxTool", "--request", string(payload))
	return err
}
