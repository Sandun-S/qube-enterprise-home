import API from '../api.js';
import Components from '../components.js';

const COMMAND_GROUPS = [
    {
        label: 'System',
        commands: [
            { value: 'ping',          label: 'Ping',              desc: 'Ping a host from the Qube' },
            { value: 'get_info',      label: 'Get System Info',   desc: 'Network IPs, MACs, WiFi, ports' },
            { value: 'restart_qube',  label: 'Restart Qube',      desc: 'Reboot the Qube device' },
            { value: 'reboot',        label: 'Reboot (alias)',    desc: 'Alias for restart_qube' },
            { value: 'shutdown',      label: 'Shutdown',          desc: 'Shut down the Qube device' },
        ]
    },
    {
        label: 'Containers & Config',
        commands: [
            { value: 'list_containers', label: 'List Containers',  desc: 'List all running Docker containers' },
            { value: 'reload_config',   label: 'Reload Config',    desc: 'Force config resync from cloud' },
            { value: 'update_sqlite',   label: 'Update SQLite',    desc: 'Alias for reload_config' },
            { value: 'get_logs',        label: 'Get Logs',         desc: 'Fetch container/service logs' },
            { value: 'restart_reader',  label: 'Restart Reader',   desc: 'Restart a specific reader container' },
            { value: 'stop_container',  label: 'Stop Container',   desc: 'Stop any container by name' },
        ]
    },
    {
        label: 'Network',
        commands: [
            { value: 'reset_ips',    label: 'Reset IPs',       desc: 'Reset all interfaces to DHCP defaults' },
            { value: 'set_eth',      label: 'Set Ethernet',    desc: 'Configure ethernet interface' },
            { value: 'set_wifi',     label: 'Set WiFi',        desc: 'Configure WiFi SSID and password' },
            { value: 'set_firewall', label: 'Set Firewall',    desc: 'Apply iptables rules' },
        ]
    },
    {
        label: 'Identity',
        commands: [
            { value: 'set_name',     label: 'Set Hostname',  desc: 'Set the device hostname' },
            { value: 'set_timezone', label: 'Set Timezone',  desc: 'Set system timezone' },
        ]
    },
    {
        label: 'Service Management',
        commands: [
            { value: 'service_add',  label: 'Add Service',    desc: 'Add a Docker service' },
            { value: 'service_rm',   label: 'Remove Service', desc: 'Remove a Docker service' },
            { value: 'service_edit', label: 'Edit Service',   desc: 'Edit service config or ports' },
        ]
    },
    {
        label: 'Backup & Restore',
        commands: [
            { value: 'backup_data',    label: 'Backup Data',    desc: 'Backup /data to CIFS/NFS share' },
            { value: 'restore_data',   label: 'Restore Data',   desc: 'Restore /data from CIFS/NFS share' },
            { value: 'backup_image',   label: 'Backup Image',   desc: 'Full disk image backup (DD)' },
            { value: 'restore_image',  label: 'Restore Image',  desc: 'Full disk image restore (DD)' },
            { value: 'repair_fs',      label: 'Repair Filesystem', desc: 'Run e2fsck filesystem repair' },
        ]
    },
    {
        label: 'File Transfer',
        commands: [
            { value: 'put_file', label: 'Push File', desc: 'Upload a file to the Qube' },
            { value: 'get_file', label: 'Pull File', desc: 'Download a file from the Qube' },
        ]
    },
];

// Payload field definitions per command type
const PAYLOAD_FIELDS = {
    ping:         [{ id: 'target', label: 'Target (Host/IP)', type: 'text', default: '8.8.8.8' }],
    get_logs:     [
        { id: 'service', label: 'Service Name (optional)', type: 'text', placeholder: 'e.g. modbus-reader' },
        { id: 'lines',   label: 'Lines',                   type: 'number', default: 50 },
    ],
    restart_reader:  [{ id: 'reader_id', label: 'Reader ID', type: 'text', placeholder: 'rd-xxxxxxxx' }],
    stop_container:  [{ id: 'name',      label: 'Container Name', type: 'text', placeholder: 'e.g. snmp-reader' }],
    set_eth:      [
        { id: 'interface', label: 'Interface',   type: 'text',   default: 'eth0' },
        { id: 'ip',        label: 'IP Address',  type: 'text',   placeholder: '192.168.1.100' },
        { id: 'netmask',   label: 'Netmask',     type: 'text',   default: '255.255.255.0' },
        { id: 'gateway',   label: 'Gateway',     type: 'text',   placeholder: '192.168.1.1' },
    ],
    set_wifi:     [
        { id: 'ssid',     label: 'SSID',     type: 'text', placeholder: 'MyNetwork' },
        { id: 'password', label: 'Password', type: 'text', placeholder: 'WiFi password' },
    ],
    set_firewall: [{ id: 'rules', label: 'iptables Rules (one per line)', type: 'textarea', placeholder: '-A INPUT -p tcp --dport 8080 -j ACCEPT' }],
    set_name:     [{ id: 'hostname', label: 'New Hostname', type: 'text', placeholder: 'qube-01' }],
    set_timezone: [{ id: 'timezone', label: 'Timezone', type: 'text', placeholder: 'Asia/Colombo', default: 'UTC' }],
    service_add:  [
        { id: 'name',  label: 'Service Name',  type: 'text', placeholder: 'my-service' },
        { id: 'image', label: 'Docker Image',  type: 'text', placeholder: 'ghcr.io/org/image:tag' },
        { id: 'ports', label: 'Ports (optional)', type: 'text', placeholder: '8080:8080' },
    ],
    service_rm:   [{ id: 'name', label: 'Service Name', type: 'text', placeholder: 'my-service' }],
    service_edit: [
        { id: 'name',   label: 'Service Name',          type: 'text', placeholder: 'my-service' },
        { id: 'config', label: 'Config JSON (optional)', type: 'textarea', placeholder: '{"ports": "9090:9090"}' },
    ],
    backup_data:   [
        { id: 'target', label: 'Share Path (CIFS/NFS)', type: 'text', placeholder: '//192.168.1.50/backup' },
        { id: 'user',   label: 'Username (optional)',   type: 'text' },
        { id: 'pass',   label: 'Password (optional)',   type: 'text' },
    ],
    restore_data:  [
        { id: 'source', label: 'Share Path (CIFS/NFS)', type: 'text', placeholder: '//192.168.1.50/backup' },
        { id: 'user',   label: 'Username (optional)',   type: 'text' },
        { id: 'pass',   label: 'Password (optional)',   type: 'text' },
    ],
    put_file:     [
        { id: 'path',    label: 'Remote Path',    type: 'text', placeholder: '/opt/qube/config.yml' },
        { id: 'content', label: 'File Content',   type: 'textarea', placeholder: 'Paste file content here' },
    ],
    get_file:     [{ id: 'path', label: 'Remote Path', type: 'text', placeholder: '/opt/qube/config.yml' }],
};

const Commands = {
    async render() {
        const qubes = await API.getQubes();
        const qubeId = window.state?.selectedQubeId || (qubes[0]?.id) || '';

        const groupOptions = COMMAND_GROUPS.map(g =>
            `<optgroup label="${g.label}">${g.commands.map(c =>
                `<option value="${c.value}">${c.label}</option>`
            ).join('')}</optgroup>`
        ).join('');

        return `
            <div class="page-header">
                <div>
                    <h1 class="page-title">Remote Commands</h1>
                    <p class="page-subtitle">Send control messages directly to Qubes</p>
                </div>
                <div class="card-header-actions">
                    <select id="cmd-qube-select" class="form-control" style="width: 200px;">
                        ${qubes.map(q => `<option value="${q.id}" ${q.id === qubeId ? 'selected' : ''}>${q.id} - ${q.location_label}</option>`).join('')}
                    </select>
                </div>
            </div>

            <div class="grid grid-2">
                <div class="card">
                    <h2 class="card-title">Send Command</h2>

                    <div class="form-group">
                        <label>Command Type</label>
                        <select id="cmd-type">${groupOptions}</select>
                    </div>
                    <div id="cmd-desc" style="font-size:12px;color:var(--text-dim);margin-bottom:12px;padding:8px;background:rgba(255,255,255,0.03);border-radius:6px;"></div>

                    <div id="cmd-payload-fields"></div>

                    <button id="btn-send-cmd" class="btn btn-primary" style="width:100%;justify-content:center;margin-top:8px;">Execute Command</button>
                </div>

                <div class="card">
                    <div class="flex-between mb-20">
                        <h2 class="card-title">Recent History</h2>
                        <button id="btn-refresh-history" class="btn btn-ghost btn-sm">Refresh</button>
                    </div>
                    <div id="cmd-history-list" class="flex flex-col gap-10">
                        <div class="text-center text-dim py-20">Select a Qube to view history</div>
                    </div>
                </div>
            </div>

            <div id="cmd-result-card" class="card hidden">
                <div class="flex-between">
                    <h2 class="card-title">Execution Result</h2>
                    <button class="btn btn-ghost btn-sm" onclick="document.getElementById('cmd-result-card').classList.add('hidden')">Close</button>
                </div>
                <pre id="cmd-result-output" class="raw-json-preview">Waiting for response...</pre>
            </div>
        `;
    },

    async init() {
        this.bindEvents();
        // Show description for initial command
        this.updateCommandUI(document.getElementById('cmd-type')?.value);
        const select = document.getElementById('cmd-qube-select');
        if (select?.value) {
            this.loadHistory(select.value);
        }
    },

    bindEvents() {
        document.getElementById('cmd-qube-select')?.addEventListener('change', (e) => {
            if (window.state) window.state.selectedQubeId = e.target.value;
            this.loadHistory(e.target.value);
        });
        document.getElementById('cmd-type')?.addEventListener('change', (e) => {
            this.updateCommandUI(e.target.value);
        });
        document.getElementById('btn-send-cmd')?.addEventListener('click', () => this.handleSendCommand());
        document.getElementById('btn-refresh-history')?.addEventListener('click', () => {
            this.loadHistory(document.getElementById('cmd-qube-select').value);
        });
    },

    updateCommandUI(type) {
        // Update description
        const desc = document.getElementById('cmd-desc');
        if (desc) {
            const allCmds = COMMAND_GROUPS.flatMap(g => g.commands);
            const cmd = allCmds.find(c => c.value === type);
            desc.textContent = cmd?.desc || '';
        }

        // Render payload fields
        const container = document.getElementById('cmd-payload-fields');
        if (!container) return;

        const fields = PAYLOAD_FIELDS[type] || [];
        if (fields.length === 0) {
            container.innerHTML = `<div class="text-dim text-center py-10" style="font-size:12px;">No additional parameters required</div>`;
            return;
        }

        container.innerHTML = fields.map(f => {
            const val = f.default !== undefined ? f.default : '';
            if (f.type === 'textarea') {
                return `<div class="form-group">
                    <label>${f.label}</label>
                    <textarea id="payload-${f.id}" style="height:80px;font-family:var(--font-mono);font-size:11px;" placeholder="${f.placeholder || ''}">${val}</textarea>
                </div>`;
            }
            return `<div class="form-group">
                <label>${f.label}</label>
                <input type="${f.type}" id="payload-${f.id}" value="${val}" placeholder="${f.placeholder || ''}">
            </div>`;
        }).join('');
    },

    async handleSendCommand() {
        const qubeId = document.getElementById('cmd-qube-select').value;
        const type = document.getElementById('cmd-type').value;
        const btn = document.getElementById('btn-send-cmd');

        const payload = {};
        const fields = PAYLOAD_FIELDS[type] || [];
        fields.forEach(f => {
            const el = document.getElementById(`payload-${f.id}`);
            if (!el) return;
            const v = el.value;
            payload[f.id] = f.type === 'number' ? (parseFloat(v) || 0) : v;
        });

        try {
            btn.disabled = true;
            btn.textContent = 'Executing...';
            const res = await API.post(`/api/v1/qubes/${qubeId}/commands`, { command: type, payload });
            Components.showAlert('Command sent successfully');
            this.showResult(res);
            this.loadHistory(qubeId);
        } catch (err) {
            Components.showAlert(err.message, 'error');
        } finally {
            btn.disabled = false;
            btn.textContent = 'Execute Command';
        }
    },

    async loadHistory(qubeId) {
        const container = document.getElementById('cmd-history-list');
        if (!qubeId || !container) return;

        try {
            const qube = await API.get(`/api/v1/qubes/${qubeId}`);
            const history = qube.recent_commands || [];

            if (history.length === 0) {
                container.innerHTML = `<div class="text-center text-dim py-20">No recent commands found</div>`;
                return;
            }

            container.innerHTML = history.map(cmd => `
                <div style="padding:10px 12px;border:1px solid var(--border);border-radius:8px;margin-bottom:6px;">
                    <div class="flex-between">
                        <div style="display:flex;gap:8px;align-items:center;">
                            <span class="badge ${this.getCmdBadgeClass(cmd.command)}">${cmd.command}</span>
                            <span style="font-size:11px;color:var(--text-dim);">${new Date(cmd.sent_at).toLocaleTimeString()}</span>
                        </div>
                        <span class="badge ${this.getStatusBadgeClass(cmd.status)}">${cmd.status}</span>
                    </div>
                </div>
            `).join('');
        } catch (err) {
            container.innerHTML = `<div class="text-error text-center py-20">${err.message}</div>`;
        }
    },

    getCmdBadgeClass(type) {
        if (type === 'ping') return 'badge-blue';
        if (type.includes('restart') || type.includes('reboot') || type.includes('shutdown')) return 'badge-warning';
        if (type.includes('backup') || type.includes('restore') || type.includes('repair')) return 'badge-error';
        return 'badge-ghost';
    },

    getStatusBadgeClass(status) {
        if (status === 'acked' || status === 'delivered') return 'badge-success';
        if (status === 'pending') return 'badge-warning';
        if (status === 'failed') return 'badge-error';
        return 'badge-blue';
    },

    showResult(res) {
        const card = document.getElementById('cmd-result-card');
        const output = document.getElementById('cmd-result-output');
        card?.classList.remove('hidden');
        if (output) output.textContent = JSON.stringify(res, null, 2);
    }
};

export default Commands;
