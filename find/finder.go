/*
Copyright (c) 2014 VMware, Inc. All Rights Reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package find

import (
	"errors"
	"path"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/list"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/mo"
)

type Finder struct {
	list.Recurser
	dc      *object.Datacenter
	folders *object.DatacenterFolders
}

func NewFinder(c *govmomi.Client, all bool) *Finder {
	return &Finder{
		Recurser: list.Recurser{
			Client: c,
			All:    all,
		},
	}
}

func (f *Finder) SetDatacenter(dc *object.Datacenter) *Finder {
	f.dc = dc
	f.folders = nil
	return f
}

type findRelativeFunc func() (object.Reference, error)

func (f *Finder) find(fn findRelativeFunc, tl bool, path ...string) ([]list.Element, error) {
	var out []list.Element

	for _, arg := range path {
		es, err := f.list(fn, tl, arg)
		if err != nil {
			return nil, err
		}

		out = append(out, es...)
	}

	return out, nil
}

func (f *Finder) list(fn findRelativeFunc, tl bool, arg string) ([]list.Element, error) {
	root := list.Element{
		Path:   "/",
		Object: object.NewRootFolder(f.Client),
	}

	parts := list.ToParts(arg)

	if len(parts) > 0 {
		switch parts[0] {
		case "..": // Not supported; many edge case, little value
			return nil, errors.New("cannot traverse up a tree")
		case ".": // Relative to whatever
			pivot, err := fn()
			if err != nil {
				return nil, err
			}

			mes, err := f.Client.Ancestors(pivot)
			if err != nil {
				return nil, err
			}

			for _, me := range mes {
				// Skip root entity in building inventory path.
				if me.Parent == nil {
					continue
				}
				root.Path = path.Join(root.Path, me.Name)
			}

			root.Object = pivot
			parts = parts[1:]
		}
	}

	f.TraverseLeafs = tl
	es, err := f.Recurse(root, parts)
	if err != nil {
		return nil, err
	}

	return es, nil
}

func (f *Finder) datacenter() (*object.Datacenter, error) {
	if f.dc == nil {
		return nil, errors.New("please specify a datacenter")
	}

	return f.dc, nil
}

func (f *Finder) dcFolders() (*object.DatacenterFolders, error) {
	if f.folders != nil {
		return f.folders, nil
	}

	dc, err := f.datacenter()
	if err != nil {
		return nil, err
	}

	folders, err := dc.Folders()
	if err != nil {
		return nil, err
	}

	f.folders = folders

	return f.folders, nil
}

func (f *Finder) dcReference() (object.Reference, error) {
	dc, err := f.datacenter()
	if err != nil {
		return nil, err
	}

	return dc, nil
}

func (f *Finder) vmFolder() (object.Reference, error) {
	folders, err := f.dcFolders()
	if err != nil {
		return nil, err
	}

	return folders.VmFolder, nil
}

func (f *Finder) hostFolder() (object.Reference, error) {
	folders, err := f.dcFolders()
	if err != nil {
		return nil, err
	}

	return folders.HostFolder, nil
}

func (f *Finder) datastoreFolder() (object.Reference, error) {
	folders, err := f.dcFolders()
	if err != nil {
		return nil, err
	}

	return folders.DatastoreFolder, nil
}

func (f *Finder) networkFolder() (object.Reference, error) {
	folders, err := f.dcFolders()
	if err != nil {
		return nil, err
	}

	return folders.NetworkFolder, nil
}

func (f *Finder) rootFolder() (object.Reference, error) {
	return object.NewRootFolder(f.Client), nil
}

func (f *Finder) ManagedObjectList(path ...string) ([]list.Element, error) {
	fn := f.rootFolder

	if f.dc != nil {
		fn = f.dcReference
	}

	if len(path) == 0 {
		path = []string{"."}
	}

	return f.find(fn, true, path...)
}

func (f *Finder) DatacenterList(path ...string) ([]*object.Datacenter, error) {
	es, err := f.find(f.rootFolder, false, path...)
	if err != nil {
		return nil, err
	}

	var dcs []*object.Datacenter
	for _, e := range es {
		ref := e.Object.Reference()
		if ref.Type == "Datacenter" {
			dcs = append(dcs, object.NewDatacenter(f.Client, ref))
		}
	}

	return dcs, nil
}

func (f *Finder) Datacenter(path string) (*object.Datacenter, error) {
	dcs, err := f.DatacenterList(path)
	if err != nil {
		return nil, err
	}

	if len(dcs) == 0 {
		return nil, &NotFoundError{"datacenter", path}
	}

	if len(dcs) > 1 {
		return nil, &MultipleFoundError{"datacenter", path}
	}

	return dcs[0], nil
}

func (f *Finder) DefaultDatacenter() (*object.Datacenter, error) {
	dc, err := f.Datacenter("*")
	if err != nil {
		return nil, toDefaultError(err)
	}

	return dc, nil
}

func (f *Finder) DatastoreList(path ...string) ([]*object.Datastore, error) {
	es, err := f.find(f.datastoreFolder, false, path...)
	if err != nil {
		return nil, err
	}

	var dss []*object.Datastore
	for _, e := range es {
		ref := e.Object.Reference()
		if ref.Type == "Datastore" {
			ds := object.NewDatastore(f.Client, ref)
			ds.InventoryPath = e.Path

			dss = append(dss, ds)
		}
	}

	return dss, nil
}

func (f *Finder) Datastore(path string) (*object.Datastore, error) {
	dss, err := f.DatastoreList(path)
	if err != nil {
		return nil, err
	}

	if len(dss) == 0 {
		return nil, &NotFoundError{"datastore", path}
	}

	if len(dss) > 1 {
		return nil, &MultipleFoundError{"datastore", path}
	}

	return dss[0], nil
}

func (f *Finder) DefaultDatastore() (*object.Datastore, error) {
	ds, err := f.Datastore("*")
	if err != nil {
		return nil, toDefaultError(err)
	}

	return ds, nil
}

func (f *Finder) HostSystemList(path ...string) ([]*object.HostSystem, error) {
	es, err := f.find(f.hostFolder, false, path...)
	if err != nil {
		return nil, err
	}

	var hss []*object.HostSystem
	for _, e := range es {
		var hs *object.HostSystem

		switch o := e.Object.(type) {
		case mo.HostSystem:
			hs = object.NewHostSystem(f.Client, o.Reference())
		case mo.ComputeResource:
			cr := object.NewComputeResource(f.Client, o.Reference())
			hosts, err := cr.Hosts()
			if err != nil {
				return nil, err
			}
			hs = object.NewHostSystem(f.Client, hosts[0])
		default:
			continue
		}

		hs.InventoryPath = e.Path
		hss = append(hss, hs)
	}

	return hss, nil
}

func (f *Finder) HostSystem(path string) (*object.HostSystem, error) {
	hss, err := f.HostSystemList(path)
	if err != nil {
		return nil, err
	}

	if len(hss) == 0 {
		return nil, &NotFoundError{"host", path}
	}

	if len(hss) > 1 {
		return nil, &MultipleFoundError{"host", path}
	}

	return hss[0], nil
}

func (f *Finder) DefaultHostSystem() (*object.HostSystem, error) {
	hs, err := f.HostSystem("*/*")
	if err != nil {
		return nil, toDefaultError(err)
	}

	return hs, nil
}

func (f *Finder) NetworkList(path ...string) ([]object.NetworkReference, error) {
	es, err := f.find(f.networkFolder, false, path...)
	if err != nil {
		return nil, err
	}

	var ns []object.NetworkReference
	for _, e := range es {
		ref := e.Object.Reference()
		switch ref.Type {
		case "Network":
			r := object.NewNetwork(f.Client, ref)
			r.InventoryPath = e.Path
			ns = append(ns, r)
		case "DistributedVirtualPortgroup":
			r := object.NewDistributedVirtualPortgroup(f.Client, ref)
			r.InventoryPath = e.Path
			ns = append(ns, r)
		}
	}

	return ns, nil
}

func (f *Finder) Network(path string) (object.NetworkReference, error) {
	networks, err := f.NetworkList(path)
	if err != nil {
		return nil, err
	}

	if len(networks) == 0 {
		return nil, &NotFoundError{"network", path}
	}

	if len(networks) > 1 {
		return nil, &MultipleFoundError{"network", path}
	}

	return networks[0], nil
}

func (f *Finder) DefaultNetwork() (object.NetworkReference, error) {
	network, err := f.Network("*")
	if err != nil {
		return nil, toDefaultError(err)
	}

	return network, nil
}

func (f *Finder) ResourcePoolList(path ...string) ([]*object.ResourcePool, error) {
	es, err := f.find(f.hostFolder, true, path...)
	if err != nil {
		return nil, err
	}

	var rps []*object.ResourcePool
	for _, e := range es {
		var rp *object.ResourcePool

		switch o := e.Object.(type) {
		case mo.ResourcePool:
			rp = object.NewResourcePool(f.Client, o.Reference())
			rp.InventoryPath = e.Path
			rps = append(rps, rp)
		}
	}

	return rps, nil
}

func (f *Finder) ResourcePool(path string) (*object.ResourcePool, error) {
	rps, err := f.ResourcePoolList(path)
	if err != nil {
		return nil, err
	}

	if len(rps) == 0 {
		return nil, &NotFoundError{"resource pool", path}
	}

	if len(rps) > 1 {
		return nil, &MultipleFoundError{"resource pool", path}
	}

	return rps[0], nil
}

func (f *Finder) DefaultResourcePool() (*object.ResourcePool, error) {
	rp, err := f.ResourcePool("*/Resources")
	if err != nil {
		return nil, toDefaultError(err)
	}

	return rp, nil
}

func (f *Finder) VirtualMachineList(path ...string) ([]*object.VirtualMachine, error) {
	es, err := f.find(f.vmFolder, false, path...)
	if err != nil {
		return nil, err
	}

	var vms []*object.VirtualMachine
	for _, e := range es {
		switch o := e.Object.(type) {
		case mo.VirtualMachine:
			vm := object.NewVirtualMachine(f.Client, o.Reference())
			vm.InventoryPath = e.Path
			vms = append(vms, vm)
		}
	}

	return vms, nil
}

func (f *Finder) VirtualMachine(path string) (*object.VirtualMachine, error) {
	vms, err := f.VirtualMachineList(path)
	if err != nil {
		return nil, err
	}

	if len(vms) == 0 {
		return nil, &NotFoundError{"vm", path}
	}

	if len(vms) > 1 {
		return nil, &MultipleFoundError{"vm", path}
	}

	return vms[0], nil
}
