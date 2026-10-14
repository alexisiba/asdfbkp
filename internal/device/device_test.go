package device

import "testing"

func TestParseLsblk(t *testing.T) {
	data := []byte(`{"blockdevices":[
	 {"name":"nvme0n1","type":"disk","size":1,"rm":false,"hotplug":false,"tran":"nvme","serial":"X","children":[
	   {"name":"nvme0n1p2","type":"part","size":1,"rm":false,"hotplug":false,"tran":"nvme","uuid":"root","mountpoint":"/"}]},
	 {"name":"sdb","type":"disk","size":8,"rm":true,"hotplug":true,"tran":"usb","serial":"ABC","vendor":"Kingston","model":"DataTraveler","children":[
	   {"name":"sdb1","type":"part","size":8000,"rm":true,"hotplug":true,"tran":"usb","label":"FOTOS","uuid":"1234-ABCD","mountpoint":"/media/u/FOTOS"},
	   {"name":"sdb2","type":"part","size":8,"rm":true,"hotplug":true,"tran":"usb","uuid":"x","mountpoint":null}]},
	 {"name":"sdc","type":"disk","size":8,"rm":true,"hotplug":true,"tran":"usb","children":[
	   {"name":"sdc1","type":"part","size":8,"rm":true,"hotplug":true,"tran":"usb","uuid":"live","mountpoint":"/"},
	   {"name":"sdc2","type":"part","size":8,"rm":true,"hotplug":true,"tran":"usb","uuid":"efi","mountpoint":"/boot/efi"}]}
	]}`)
	devs, err := parseLsblk(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 1 {
		t.Fatalf("want 1 device, got %d: %+v", len(devs), devs)
	}
	d := devs[0]
	if d.Name != "FOTOS" || d.Path != "/media/u/FOTOS" || d.ID != "serial:ABC|uuid:1234-ABCD" {
		t.Errorf("unexpected device: %+v", d)
	}
}

func TestParseADBDevices(t *testing.T) {
	out := []byte("* daemon started successfully\nList of devices attached\n" +
		"R58M123  device usb:1-1 product:a51 model:SM_A515F device:a51 transport_id:1\n" +
		"192.168.1.5:5555  unauthorized transport_id:2\n")
	devs := parseADBDevices(out)
	if len(devs) != 2 {
		t.Fatalf("want 2 devices, got %d", len(devs))
	}
	if devs[0].Name != "SM A515F" || !devs[0].Ready() || devs[0].ID != "adb:R58M123" {
		t.Errorf("unexpected usb device: %+v", devs[0])
	}
	if devs[1].Ready() || !devs[1].Wireless {
		t.Errorf("unexpected wireless device: %+v", devs[1])
	}
}

func TestDedupeMTP(t *testing.T) {
	mtp := []Device{{Kind: KindMTP, Path: "/run/user/1000/gvfs/mtp:host=SAMSUNG_Android_R58M123"}}
	adb := []Device{{Kind: KindADB, ID: "adb:R58M123", Path: "192.168.1.5:5555", State: "device"}}
	if got := dedupeMTP(mtp, adb); len(got) != 0 {
		t.Errorf("MTP mount should be hidden when ADB is available: %+v", got)
	}
	adb[0].State = "unauthorized"
	if got := dedupeMTP(mtp, adb); len(got) != 1 {
		t.Errorf("MTP mount should stay when ADB is not ready")
	}
}

func TestFolderName(t *testing.T) {
	for in, want := range map[string]string{
		"SM A515F":         "SM-A515F",
		"Kingston  / USB ": "Kingston-USB",
		"FOTOS":            "FOTOS",
		"???":              "device",
	} {
		if got := (Device{Name: in}).FolderName(); got != want {
			t.Errorf("FolderName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPrintable(t *testing.T) {
	if got := Printable("USB\x1b[2J\u202eFOTOS\n"); got != "USB?[2J?FOTOS?" {
		t.Errorf("Printable = %q", got)
	}
}
