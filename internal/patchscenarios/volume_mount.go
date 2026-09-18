package patchscenarios

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchtest"
)

var volumeMountAssertions = []string{
	"mount.volume-ref", "mount.volume-ref.by-id", "mount.template-ref",
	"mount.data-path", "mount.readonly", "mount.template.reuse", "mount.template.isolation",
	"mount.fork", "mount.reclaim.delete", "mount.reclaim.retain",
	"mount.invalid-reference", "mount.cleanup",
}

type volumeMountOwned struct {
	instances         []string
	tools             []string
	volumes           []string
	templates         []string
	bootstrapInstance string
	bootstrapPath     string
}

func runVolumeMountLifecycle(s *patchtest.Session) error {
	storage, err := volumeMountStorageFromEnv()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(s.Context, 18*time.Minute)
	defer cancel()
	owned := &volumeMountOwned{}
	s.Cleanup(func(cleanupCtx context.Context) error { return owned.cleanup(s, cleanupCtx) })

	stamp := fmt.Sprintf("%d", time.Now().UnixNano())
	name := "agr-volume-mount-e2e-" + stamp
	keyA, keyB, reclaimKey := "session-a-"+stamp, "session-b-"+stamp, "reclaim-"+stamp
	dataToken, sharedToken := "volume-"+stamp, "shared-"+stamp
	storageRoot := "/ags-vol-test/agr-e2e/" + name
	bootstrapToolID, err := mountCreateTool(s, ctx, owned, storage, name+"-bootstrap", []any{
		map[string]any{
			"Name": "bootstrap", "MountPath": "/mnt/bootstrap", "ReadOnly": false,
			"StorageSource": map[string]any{"Cfs": map[string]any{"FileSystemId": storage.cfsFileSystemID, "Path": "/"}},
		},
	})
	if err != nil {
		return err
	}
	bootstrapInstance, err := mountStartInstance(s, ctx, owned, bootstrapToolID, "bootstrap-"+stamp)
	if err != nil {
		return err
	}
	owned.bootstrapInstance = bootstrapInstance
	owned.bootstrapPath = "/mnt/bootstrap" + storageRoot
	if _, err := mountExecAsRoot(s, ctx, bootstrapInstance, fmt.Sprintf(
		"mkdir -p -- %s/volume %s/shared/%s %s/shared/%s %s/reclaim-delete/%s %s/reclaim-retain/%s; chmod -R 0777 -- %s",
		owned.bootstrapPath, owned.bootstrapPath, keyA, owned.bootstrapPath, keyB,
		owned.bootstrapPath, reclaimKey, owned.bootstrapPath, reclaimKey, owned.bootstrapPath,
	)); err != nil {
		return err
	}

	volumeName := name + "-volume"
	volumeID, err := mountCreateVolume(s, ctx, owned, storage, volumeName, storageRoot+"/volume")
	if err != nil {
		return err
	}
	sharedTemplateName := name + "-shared"
	sharedTemplateID, err := mountCreateTemplate(s, ctx, owned, storage, sharedTemplateName, "ReadWriteMany", "Retain", storageRoot+"/shared/${reuse_key}")
	if err != nil {
		return err
	}
	deleteTemplateName := name + "-delete"
	deleteTemplateID, err := mountCreateTemplate(s, ctx, owned, storage, deleteTemplateName, "ReadWriteOnce", "Delete", storageRoot+"/reclaim-delete/${reuse_key}")
	if err != nil {
		return err
	}
	retainTemplateName := name + "-retain"
	retainTemplateID, err := mountCreateTemplate(s, ctx, owned, storage, retainTemplateName, "ReadWriteOnce", "Retain", storageRoot+"/reclaim-retain/${reuse_key}")
	if err != nil {
		return err
	}

	mounts := mountDefinitions(volumeName, volumeID, sharedTemplateName)
	toolID, err := mountCreateTool(s, ctx, owned, storage, name+"-tool", mounts)
	if err != nil {
		return err
	}
	if err := mountAssertToolReferences(s, ctx, toolID, volumeName, volumeID, sharedTemplateName); err != nil {
		return err
	}

	instanceA, err := mountStartInstance(s, ctx, owned, toolID, keyA)
	if err != nil {
		return err
	}
	if _, err := mountExec(s, ctx, instanceA, fmt.Sprintf(
		"set -eu; printf %s > /mnt/by-name/probe; test \"$(cat /mnt/by-id/probe)\" = %s; printf %s > /mnt/shared/probe; if printf denied > /mnt/by-id/denied 2>/dev/null; then exit 91; fi",
		dataToken, dataToken, sharedToken,
	)); err != nil {
		return err
	}
	if err := s.Assert("mount.data-path", true); err != nil {
		return err
	}
	if err := s.Assert("mount.readonly", true); err != nil {
		return err
	}
	sharedA, err := mountWaitDerived(s, ctx, sharedTemplateID, keyA)
	if err != nil {
		return err
	}
	owned.addVolume(sharedA)

	instanceB, err := mountStartInstance(s, ctx, owned, toolID, keyA)
	if err != nil {
		return err
	}
	stdout, err := mountExec(s, ctx, instanceB, "set -eu; cat /mnt/shared/probe")
	if err != nil {
		return err
	}
	sharedB, err := mountWaitDerived(s, ctx, sharedTemplateID, keyA)
	if err != nil {
		return err
	}
	owned.addVolume(sharedB)
	if err := s.Assert("mount.template.reuse", sharedA == sharedB && strings.Contains(stdout, sharedToken)); err != nil {
		return err
	}

	instanceC, err := mountStartInstance(s, ctx, owned, toolID, keyB)
	if err != nil {
		return err
	}
	if _, err := mountExec(s, ctx, instanceC, "set -eu; test ! -e /mnt/shared/probe; printf isolated > /mnt/shared/probe"); err != nil {
		return err
	}
	sharedC, err := mountWaitDerived(s, ctx, sharedTemplateID, keyB)
	if err != nil {
		return err
	}
	owned.addVolume(sharedC)
	if err := s.Assert("mount.template.isolation", sharedC != sharedA); err != nil {
		return err
	}

	forkedToolID, err := mountForkTool(s, ctx, owned, toolID, name+"-fork")
	if err != nil {
		return err
	}
	if err := mountAssertToolReferences(s, ctx, forkedToolID, volumeName, volumeID, sharedTemplateName); err != nil {
		return err
	}
	forkedInstance, err := mountStartInstance(s, ctx, owned, forkedToolID, keyA)
	if err != nil {
		return err
	}
	stdout, err = mountExec(s, ctx, forkedInstance, "set -eu; cat /mnt/by-name/probe; cat /mnt/shared/probe")
	if err != nil {
		return err
	}
	if err := s.Assert("mount.fork", strings.Contains(stdout, dataToken) && strings.Contains(stdout, sharedToken)); err != nil {
		return err
	}

	invalidToolID, err := mountCreateTool(s, ctx, owned, storage, name+"-invalid", []any{
		map[string]any{
			"Name": "missing-metadata", "MountPath": "/mnt/missing", "ReadOnly": false,
			"VolumeTemplate": map[string]any{"VolumeTemplateName": sharedTemplateName, "ReuseKey": "${metadata.missing_key}"},
		},
	})
	if err != nil {
		return err
	}
	failed, callErr := volumeCLI(s, ctx, "instance", "create", "--wait", "--timeout", "5m", "--tool-id", invalidToolID)
	owned.addInstance(volumeResourceID(failed, "InstanceId"))
	if err := mountTrackToolInstances(s, ctx, owned, invalidToolID); err != nil {
		return err
	}
	invalidReferenceFailure := failed.Failure != nil &&
		strings.HasPrefix(failed.Failure.Code, "InvalidParameter") && failed.Failure.Kind == "usage"
	if err := s.Assert("mount.invalid-reference", callErr != nil && failed.Status != "succeeded" && invalidReferenceFailure); err != nil {
		return err
	}

	deleteToolID, err := mountCreateTool(s, ctx, owned, storage, name+"-rd", []any{
		map[string]any{"Name": "delete-derived", "MountPath": "/mnt/delete", "ReadOnly": false, "VolumeTemplate": map[string]any{"VolumeTemplateName": deleteTemplateName, "ReuseKey": "${metadata.session_id}"}},
	})
	if err != nil {
		return err
	}
	deleteInstance, err := mountStartInstance(s, ctx, owned, deleteToolID, reclaimKey)
	if err != nil {
		return err
	}
	if _, err := mountExec(s, ctx, deleteInstance, "printf delete > /mnt/delete/probe"); err != nil {
		return err
	}
	deleteVolumeID, err := mountWaitDerived(s, ctx, deleteTemplateID, reclaimKey)
	if err != nil {
		return err
	}
	owned.addVolume(deleteVolumeID)
	if err := mountDeleteInstance(s, ctx, deleteInstance); err != nil {
		return err
	}
	if err := mountWaitVolumeAbsent(s, ctx, deleteVolumeID); err != nil {
		return err
	}
	if err := s.Assert("mount.reclaim.delete", true); err != nil {
		return err
	}

	retainToolID, err := mountCreateTool(s, ctx, owned, storage, name+"-rr", []any{
		map[string]any{"Name": "retain-derived", "MountPath": "/mnt/retain", "ReadOnly": false, "VolumeTemplate": map[string]any{"VolumeTemplateName": retainTemplateName, "ReuseKey": "${metadata.session_id}"}},
	})
	if err != nil {
		return err
	}
	retainInstance, err := mountStartInstance(s, ctx, owned, retainToolID, reclaimKey)
	if err != nil {
		return err
	}
	if _, err := mountExec(s, ctx, retainInstance, "printf retain > /mnt/retain/probe"); err != nil {
		return err
	}
	retainVolumeID, err := mountWaitDerived(s, ctx, retainTemplateID, reclaimKey)
	if err != nil {
		return err
	}
	owned.addVolume(retainVolumeID)
	if err := mountDeleteInstance(s, ctx, retainInstance); err != nil {
		return err
	}
	if err := mountWaitVolumePresent(s, ctx, retainVolumeID); err != nil {
		return err
	}
	if err := s.Assert("mount.reclaim.retain", true); err != nil {
		return err
	}

	for _, id := range []string{forkedInstance, instanceC, instanceB, instanceA} {
		if err := mountDeleteInstance(s, ctx, id); err != nil {
			return err
		}
	}
	if err := owned.cleanup(s, ctx); err != nil {
		return err
	}
	return s.Assert("mount.cleanup", true)
}

func volumeMountStorageFromEnv() (volumeStorage, error) {
	fixture := volumeStorage{
		cfsFileSystemID: os.Getenv("AGR_VOLUME_CFS_FILESYSTEM_ID"),
		toolType:        os.Getenv("AGR_VOLUME_TOOL_TYPE"),
		toolRoleArn:     os.Getenv("AGR_VOLUME_TOOL_ROLE_ARN"),
	}
	for name, value := range map[string]string{
		"AGR_VOLUME_CFS_FILESYSTEM_ID": fixture.cfsFileSystemID,
		"AGR_VOLUME_TOOL_TYPE":         fixture.toolType,
	} {
		if strings.TrimSpace(value) == "" {
			return volumeStorage{}, fmt.Errorf("%s must identify a reviewed disposable mount fixture", name)
		}
	}
	return fixture, nil
}

func mountDefinitions(volumeName, volumeID, sharedTemplateName string) []any {
	return []any{
		map[string]any{"Name": "volume-by-name", "MountPath": "/mnt/by-name", "ReadOnly": false, "Volume": map[string]any{"VolumeName": volumeName}},
		map[string]any{"Name": "volume-by-id", "MountPath": "/mnt/by-id", "ReadOnly": true, "Volume": map[string]any{"VolumeId": volumeID}},
		map[string]any{"Name": "shared-derived", "MountPath": "/mnt/shared", "ReadOnly": false, "VolumeTemplate": map[string]any{"VolumeTemplateName": sharedTemplateName, "ReuseKey": "${metadata.session_id}"}},
	}
}

func mountCreateVolume(s *patchtest.Session, ctx context.Context, owned *volumeMountOwned, storage volumeStorage, name, storagePath string) (string, error) {
	result, err := volumeCall(s, ctx, "volume.create", map[string]any{
		"VolumeName": name, "AccessMode": "ReadWriteMany", "StorageType": "Cfs",
		"Storage": map[string]any{"Cfs": map[string]any{"FileSystemId": storage.cfsFileSystemID, "Path": storagePath}},
		"Tags":    volumeTags(volumeTagKey, "mount-lifecycle"),
	})
	id := volumeString(volumeObject(result.Data["Volume"]), "VolumeId")
	if id != "" {
		owned.volumes = append(owned.volumes, id)
	}
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", errors.New("volume create returned no VolumeId")
	}
	return id, mountWaitResourceActive(s, ctx, "volume.list", "VolumeSet", "VolumeIds", "VolumeId", id)
}

func mountCreateTemplate(s *patchtest.Session, ctx context.Context, owned *volumeMountOwned, storage volumeStorage, name, accessMode, reclaim, pathPattern string) (string, error) {
	result, err := volumeCall(s, ctx, "volume-template.create", map[string]any{
		"VolumeTemplateName": name, "AccessMode": accessMode, "ReclaimPolicy": reclaim, "StorageType": "Cfs",
		"StorageSpec":       map[string]any{"Cfs": map[string]any{"FileSystemId": storage.cfsFileSystemID, "PathPattern": pathPattern}},
		"VolumeNamePattern": "${reuse_key}", "Tags": volumeTags(volumeTagKey, "mount-lifecycle"),
	})
	id := volumeString(volumeObject(result.Data["VolumeTemplate"]), "VolumeTemplateId")
	if id != "" {
		owned.templates = append(owned.templates, id)
	}
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", errors.New("volume template create returned no VolumeTemplateId")
	}
	return id, mountWaitResourceActive(s, ctx, "volume-template.list", "VolumeTemplateSet", "VolumeTemplateIds", "VolumeTemplateId", id)
}

func mountCreateTool(s *patchtest.Session, ctx context.Context, owned *volumeMountOwned, storage volumeStorage, name string, mounts []any) (string, error) {
	request := map[string]any{
		"ToolName": name, "ToolType": storage.toolType,
		"NetworkConfiguration": map[string]any{"NetworkMode": "PUBLIC"}, "StorageMounts": mounts,
	}
	if storage.toolRoleArn != "" {
		request["RoleArn"] = storage.toolRoleArn
	}
	result, err := volumeCall(s, ctx, "tool.create", request)
	id := volumeString(result.Data, "ToolId")
	if id != "" {
		owned.tools = append(owned.tools, id)
	}
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", errors.New("tool create returned no ToolId")
	}
	return id, mountWaitToolActive(s, ctx, id)
}

func mountForkTool(s *patchtest.Session, ctx context.Context, owned *volumeMountOwned, sourceID, name string) (string, error) {
	result, err := volumeCLI(s, ctx, "tool", "fork", sourceID, "--tool-name", name)
	id := volumeString(result.Data, "ToolId")
	if id != "" {
		owned.tools = append(owned.tools, id)
	}
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", errors.New("tool fork returned no ToolId")
	}
	return id, mountWaitToolActive(s, ctx, id)
}

func mountStartInstance(s *patchtest.Session, ctx context.Context, owned *volumeMountOwned, toolID, reuseKey string) (string, error) {
	metadata, err := json.Marshal([]any{map[string]any{"Name": "session_id", "Value": reuseKey}})
	if err != nil {
		return "", err
	}
	result, err := volumeCLI(s, ctx, "instance", "create", "--wait", "--timeout", "10m", "--tool-id", toolID, "--metadata", string(metadata))
	id := volumeResourceID(result, "InstanceId")
	owned.addInstance(id)
	if err != nil {
		if id == "" {
			if trackErr := mountTrackToolInstances(s, ctx, owned, toolID); trackErr != nil {
				return "", errors.Join(err, fmt.Errorf("track instances after failed create: %w", trackErr))
			}
		}
		return "", err
	}
	if id == "" || volumeString(result.Data, "Status") != "RUNNING" {
		return "", errors.New("instance did not reach RUNNING")
	}
	return id, nil
}

func mountExec(s *patchtest.Session, ctx context.Context, instanceID, script string) (string, error) {
	return mountExecAs(s, ctx, instanceID, "", script)
}

func mountExecAsRoot(s *patchtest.Session, ctx context.Context, instanceID, script string) (string, error) {
	return mountExecAs(s, ctx, instanceID, "root", script)
}

func mountExecAs(s *patchtest.Session, ctx context.Context, instanceID, user, script string) (string, error) {
	args := []string{"-o", "json", "instance", "exec", instanceID}
	if user != "" {
		args = append(args, "--user", user)
	}
	args = append(args, "--", "sh", "-lc", script)
	raw, callErr := s.CLI(ctx, args...)
	var result volumeEnvelope
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return "", errors.New("instance exec returned an invalid CLI envelope")
	}
	if callErr != nil || result.Status != "succeeded" || result.Failure != nil {
		stderr := volumeString(result.Data, "Stderr")
		if result.Failure != nil {
			return "", fmt.Errorf("instance exec failed (%s/%s: %s; stderr=%q)", result.Failure.Code, result.Failure.Kind, result.Failure.Message, stderr)
		}
		return "", fmt.Errorf("instance exec failed (status=%q; stderr=%q)", result.Status, stderr)
	}
	return volumeString(result.Data, "Stdout"), nil
}

func mountAssertToolReferences(s *patchtest.Session, ctx context.Context, toolID, volumeName, volumeID, templateName string) error {
	result, err := volumeCall(s, ctx, "tool.list", map[string]any{"ToolIds": []string{toolID}})
	if err != nil {
		return err
	}
	items, ok := result.Data["Items"].([]any)
	if !ok {
		return errors.New("tool list Items is not an array")
	}
	stored := volumeRow(items, "ToolId", toolID)
	if stored == nil {
		return errors.New("tool readback missing")
	}
	rows, ok := stored["StorageMounts"].([]any)
	if !ok {
		return errors.New("tool StorageMounts is not an array")
	}
	mounts := map[string]map[string]any{}
	for _, row := range rows {
		mount := volumeObject(row)
		mounts[volumeString(mount, "Name")] = mount
	}
	byName := mounts["volume-by-name"]
	if err := s.Assert("mount.volume-ref", byName != nil && volumeString(volumeObject(byName["Volume"]), "VolumeName") == volumeName); err != nil {
		return err
	}
	byID := mounts["volume-by-id"]
	if err := s.Assert("mount.volume-ref.by-id", byID != nil && byID["ReadOnly"] == true && volumeString(volumeObject(byID["Volume"]), "VolumeId") == volumeID); err != nil {
		return err
	}
	derived := mounts["shared-derived"]
	return s.Assert("mount.template-ref", derived != nil &&
		volumeString(volumeObject(derived["VolumeTemplate"]), "VolumeTemplateName") == templateName &&
		volumeString(volumeObject(derived["VolumeTemplate"]), "ReuseKey") == "${metadata.session_id}")
}

func mountWaitDerived(s *patchtest.Session, ctx context.Context, templateID, reuseKey string) (string, error) {
	var id string
	err := mountPoll(ctx, func() (bool, error) {
		rows, _, err := volumeQuery(s, ctx, "volume.list", "VolumeSet", map[string]any{
			"Filters": []any{map[string]any{"Name": "volume-template-id", "Values": []string{templateID}}}, "Limit": 100,
		})
		if err != nil {
			return false, err
		}
		for _, row := range rows {
			volume := volumeObject(row)
			if volumeString(volume, "VolumeTemplateId") == templateID && volumeString(volume, "ReuseKey") == reuseKey {
				id = volumeString(volume, "VolumeId")
				return id != "" && volumeString(volume, "Status") == "ACTIVE", nil
			}
		}
		return false, nil
	})
	return id, err
}

func mountWaitResourceActive(s *patchtest.Session, ctx context.Context, command, setKey, idsField, idField, id string) error {
	return mountPoll(ctx, func() (bool, error) {
		rows, _, err := volumeQuery(s, ctx, command, setKey, map[string]any{idsField: []string{id}})
		if err != nil {
			return false, err
		}
		stored := volumeRow(rows, idField, id)
		return stored != nil && volumeString(stored, "Status") == "ACTIVE", nil
	})
}

func mountWaitToolActive(s *patchtest.Session, ctx context.Context, id string) error {
	return mountPoll(ctx, func() (bool, error) {
		result, err := volumeCall(s, ctx, "tool.list", map[string]any{"ToolIds": []string{id}})
		if err != nil {
			return false, err
		}
		items, ok := result.Data["Items"].([]any)
		if !ok {
			return false, errors.New("tool list Items is not an array")
		}
		stored := volumeRow(items, "ToolId", id)
		return stored != nil && volumeString(stored, "Status") == "ACTIVE", nil
	})
}

func mountWaitVolumePresent(s *patchtest.Session, ctx context.Context, id string) error {
	return mountPoll(ctx, func() (bool, error) {
		rows, total, err := volumeQuery(s, ctx, "volume.list", "VolumeSet", map[string]any{"VolumeIds": []string{id}})
		return err == nil && total == 1 && volumeRow(rows, "VolumeId", id) != nil, err
	})
}

func mountWaitVolumeAbsent(s *patchtest.Session, ctx context.Context, id string) error {
	return mountPoll(ctx, func() (bool, error) {
		rows, total, err := volumeQuery(s, ctx, "volume.list", "VolumeSet", map[string]any{"VolumeIds": []string{id}})
		return err == nil && total == 0 && len(rows) == 0, err
	})
}

func mountPoll(ctx context.Context, check func() (bool, error)) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		ok, err := check()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func mountTrackToolInstances(s *patchtest.Session, ctx context.Context, owned *volumeMountOwned, toolID string) error {
	result, err := volumeCall(s, ctx, "instance.list", map[string]any{"ToolId": toolID, "Limit": 100})
	if err != nil {
		return err
	}
	items, ok := result.Data["Items"].([]any)
	if !ok {
		return errors.New("instance list Items is not an array")
	}
	for _, item := range items {
		id := volumeString(volumeObject(item), "InstanceId")
		if id != "" && !slices.Contains(owned.instances, id) {
			owned.instances = append(owned.instances, id)
		}
	}
	return nil
}

func (o *volumeMountOwned) cleanup(s *patchtest.Session, ctx context.Context) error {
	var errs []error
	for i := len(o.instances) - 1; i >= 0; i-- {
		if o.instances[i] == o.bootstrapInstance {
			continue
		}
		if err := mountDeleteInstance(s, ctx, o.instances[i]); err != nil {
			errs = append(errs, err)
		}
	}
	if o.bootstrapInstance != "" {
		if o.bootstrapPath != "" {
			if _, err := mountExecAsRoot(s, ctx, o.bootstrapInstance, "rm -rf -- "+o.bootstrapPath); err != nil {
				errs = append(errs, err)
			} else {
				o.bootstrapPath = ""
			}
		}
		if err := mountDeleteInstance(s, ctx, o.bootstrapInstance); err != nil {
			errs = append(errs, err)
		} else {
			o.bootstrapInstance = ""
		}
	}
	for i := len(o.tools) - 1; i >= 0; i-- {
		if err := mountDeleteTool(s, ctx, o.tools[i]); err != nil {
			errs = append(errs, err)
		}
	}
	for _, templateID := range o.templates {
		ids, err := mountDerivedIDs(s, ctx, templateID)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, id := range ids {
			o.addVolume(id)
		}
	}
	for i := len(o.volumes) - 1; i >= 0; i-- {
		if err := mountDeleteVolume(s, ctx, o.volumes[i]); err != nil {
			errs = append(errs, err)
		}
	}
	for i := len(o.templates) - 1; i >= 0; i-- {
		if err := mountDeleteTemplate(s, ctx, o.templates[i]); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func mountDerivedIDs(s *patchtest.Session, ctx context.Context, templateID string) ([]string, error) {
	rows, _, err := volumeQuery(s, ctx, "volume.list", "VolumeSet", map[string]any{
		"Filters": []any{map[string]any{"Name": "volume-template-id", "Values": []string{templateID}}}, "Limit": 100,
	})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if id := volumeString(volumeObject(row), "VolumeId"); id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func mountDeleteInstance(s *patchtest.Session, ctx context.Context, id string) error {
	result, err := volumeCall(s, ctx, "instance.list", map[string]any{"InstanceIds": []string{id}, "Limit": 100})
	if err != nil {
		return err
	}
	items, ok := result.Data["Items"].([]any)
	if !ok {
		return errors.New("instance list Items is not an array")
	}
	stored := volumeRow(items, "InstanceId", id)
	if stored != nil && volumeString(stored, "Status") != "STOPPED" {
		if _, err := volumeCLI(s, ctx, "instance", "delete", id, "--wait"); err != nil {
			return err
		}
	}
	return mountPoll(ctx, func() (bool, error) {
		result, err = volumeCall(s, ctx, "instance.list", map[string]any{"InstanceIds": []string{id}, "Limit": 100})
		if err != nil {
			return false, err
		}
		items, ok = result.Data["Items"].([]any)
		if !ok {
			return false, errors.New("instance list Items is not an array")
		}
		stored := volumeRow(items, "InstanceId", id)
		return stored == nil || volumeString(stored, "Status") == "STOPPED", nil
	})
}

func mountDeleteTool(s *patchtest.Session, ctx context.Context, id string) error {
	result, err := volumeCall(s, ctx, "tool.list", map[string]any{"ToolIds": []string{id}})
	if err != nil {
		return err
	}
	items, ok := result.Data["Items"].([]any)
	if !ok {
		return errors.New("tool list Items is not an array")
	}
	if volumeRow(items, "ToolId", id) != nil {
		if _, err := volumeCLI(s, ctx, "tool", "delete", id, "--wait", "--yes"); err != nil {
			return err
		}
	}
	result, err = volumeCall(s, ctx, "tool.list", map[string]any{"ToolIds": []string{id}})
	if err != nil {
		return err
	}
	items, ok = result.Data["Items"].([]any)
	if !ok || volumeRow(items, "ToolId", id) != nil {
		return errors.New("tool absence not confirmed")
	}
	return nil
}

func mountDeleteVolume(s *patchtest.Session, ctx context.Context, id string) error {
	rows, total, err := volumeQuery(s, ctx, "volume.list", "VolumeSet", map[string]any{"VolumeIds": []string{id}})
	if err != nil {
		return err
	}
	if total != 0 || len(rows) != 0 {
		if _, err := volumeCall(s, ctx, "volume.delete", map[string]any{"VolumeId": id}); err != nil {
			return err
		}
	}
	return volumeAbsent(s, ctx, "volume.list", "VolumeSet", "VolumeIds", id)
}

func mountDeleteTemplate(s *patchtest.Session, ctx context.Context, id string) error {
	rows, total, err := volumeQuery(s, ctx, "volume-template.list", "VolumeTemplateSet", map[string]any{"VolumeTemplateIds": []string{id}})
	if err != nil {
		return err
	}
	if total != 0 || len(rows) != 0 {
		if _, err := volumeCall(s, ctx, "volume-template.delete", map[string]any{"VolumeTemplateId": id}); err != nil {
			return err
		}
	}
	return volumeAbsent(s, ctx, "volume-template.list", "VolumeTemplateSet", "VolumeTemplateIds", id)
}

func (o *volumeMountOwned) addVolume(id string) {
	if id != "" && !slices.Contains(o.volumes, id) {
		o.volumes = append(o.volumes, id)
	}
}

func (o *volumeMountOwned) addInstance(id string) {
	if id != "" && !slices.Contains(o.instances, id) {
		o.instances = append(o.instances, id)
	}
}
