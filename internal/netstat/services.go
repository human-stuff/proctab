package netstat

// Service is a well-known service that the Ports tab always shows,
// green when something listens on one of its ports, red otherwise.
type Service struct {
	Name  string
	Ports []uint16
	Procs []string // process names that indicate the service (comm)
}

// KnownServices is the default watch list. Order is display order.
var KnownServices = []Service{
	{"ssh", []uint16{22}, []string{"sshd"}},
	{"http", []uint16{80, 8080}, nil},
	{"https", []uint16{443, 8443}, nil},
	{"nginx", nil, []string{"nginx"}},
	{"apache", nil, []string{"apache2", "httpd"}},
	{"dns", []uint16{53}, []string{"named", "dnsmasq", "systemd-resolve", "unbound"}},
	{"smtp", []uint16{25, 587, 465}, []string{"master", "exim4", "sendmail"}},
	{"ftp", []uint16{21}, []string{"vsftpd", "proftpd"}},
	{"mysql", []uint16{3306}, []string{"mysqld", "mariadbd"}},
	{"postgres", []uint16{5432}, []string{"postgres"}},
	{"redis", []uint16{6379}, []string{"redis-server"}},
	{"mongodb", []uint16{27017}, []string{"mongod"}},
	{"memcached", []uint16{11211}, []string{"memcached"}},
	{"rabbitmq", []uint16{5672, 15672}, []string{"beam.smp"}},
	{"elastic", []uint16{9200}, nil},
	{"docker", []uint16{2375, 2376}, []string{"dockerd"}},
	{"k8s-api", []uint16{6443}, []string{"kube-apiserver"}},
	{"cups", []uint16{631}, []string{"cupsd"}},
	{"samba", []uint16{445, 139}, []string{"smbd"}},
	{"vnc", []uint16{5900}, nil},
	{"rdp", []uint16{3389}, []string{"xrdp"}},
}

// ServiceForPort returns the well-known name of a port, or "".
func ServiceForPort(p uint16) string {
	for _, s := range KnownServices {
		for _, sp := range s.Ports {
			if sp == p {
				return s.Name
			}
		}
	}
	if n, ok := extraPorts[p]; ok {
		return n
	}
	return ""
}

var extraPorts = map[uint16]string{
	23: "telnet", 67: "dhcp", 68: "dhcp", 110: "pop3", 111: "rpcbind", 123: "ntp",
	143: "imap", 161: "snmp", 389: "ldap", 993: "imaps", 995: "pop3s",
	1883: "mqtt", 3000: "dev-http", 5000: "dev-http", 5353: "mdns", 5355: "llmnr",
	8000: "dev-http", 9090: "prometheus", 9100: "node-exp", 2049: "nfs",
}
