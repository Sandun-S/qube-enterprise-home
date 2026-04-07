import API from '../api.js';
import Components from '../components.js';

const Commands = {
    async render() {
        const qubes = await API.getQubes();
        const qubeId = window.state?.selectedQubeId || (qubes[0]?.id) || '';
        
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
                    <h2 class="card-title">🚀 Send Command</h2>
                    <div class="form-group">
                        <label>Command Type</label>
                        <select id="cmd-type">
                            <optgroup label="System">
                                <option value="ping">Ping</option>
                                <option value="get_info">Get System Info</option>
                                <option value="restart_qube">Restart Qube</option>
                            </optgroup>
                            <optgroup label="Containers">
                                <option value="list_containers">List Containers</option>
                                <option value="reload_config">Reload Config</option>
                                <option value="get_logs">Get Logs</option>
                            </optgroup>
                            <optgroup label="Network">
                                <option value="reset_ips">Reset IPs</option>
                                <option value="get_interfaces">Get Interfaces</option>
                            </optgroup>
                        </select>
                    </div>
                    
                    <div id="cmd-payload-fields">
                        <!-- Dynamic fields based on command -->
                        <div class="form-group">
                            <label>Target (Host/IP)</label>
                            <input type="text" id="payload-target" value="8.8.8.8">
                        </div>
                    </div>

                    <button id="btn-send-cmd" class="btn btn-primary" style="width: 100%; justify-content: center;">Execute Command</button>
                </div>

                <div class="card">
                    <div class="flex-between mb-20">
                        <h2 class="card-title">🕒 Recent History</h2>
                        <button id="btn-refresh-history" class="btn btn-ghost btn-sm">🔄 Refresh</button>
                    </div>
                    <div id="cmd-history-list" class="flex flex-col gap-10">
                        <div class="text-center text-dim py-20">Select a Qube to view history</div>
                    </div>
                </div>
            </div>

            <div id="cmd-result-card" class="card hidden">
                <div class="flex-between">
                    <h2 class="card-title">📄 Execution Result</h2>
                    <button class="btn btn-ghost btn-sm" onclick="this.closest('#cmd-result-card').classList.add('hidden')">Close</button>
                </div>
                <pre id="cmd-result-output" class="raw-json-preview">Waiting for response...</pre>
            </div>
        `;
    },

    async init() {
        this.bindEvents();
        const select = document.getElementById('cmd-qube-select');
        if (select && select.value) {
            this.loadHistory(select.value);
        }
    },

    bindEvents() {
        const qubeSelect = document.getElementById('cmd-qube-select');
        const cmdType = document.getElementById('cmd-type');
        const btnSend = document.getElementById('btn-send-cmd');
        const btnRefresh = document.getElementById('btn-refresh-history');

        qubeSelect.addEventListener('change', (e) => {
            if (window.state) window.state.selectedQubeId = e.target.value;
            this.loadHistory(e.target.value);
        });

        cmdType.addEventListener('change', (e) => this.updatePayloadFields(e.target.value));

        btnSend.addEventListener('click', () => this.handleSendCommand());
        btnRefresh.addEventListener('click', () => this.loadHistory(qubeSelect.value));
    },

    updatePayloadFields(type) {
        const container = document.getElementById('cmd-payload-fields');
        let html = '';

        switch (type) {
            case 'ping':
                html = `<div class="form-group"><label>Target (Host/IP)</label><input type="text" id="payload-target" value="8.8.8.8"></div>`;
                break;
            case 'get_logs':
                html = `
                    <div class="form-group"><label>Service Name (optional)</label><input type="text" id="payload-service" placeholder="e.g. modbus-reader"></div>
                    <div class="form-group"><label>Lines</label><input type="number" id="payload-lines" value="50"></div>
                `;
                break;
            case 'restart_reader':
                html = `<div class="form-group"><label>Reader ID</label><input type="text" id="payload-reader_id" placeholder="rd-xxx"></div>`;
                break;
            default:
                html = `<div class="text-dim text-center py-10" style="font-size: 12px;">No additional parameters required</div>`;
        }

        container.innerHTML = html;
    },

    async handleSendCommand() {
        const qubeId = document.getElementById('cmd-qube-select').value;
        const type = document.getElementById('cmd-type').value;
        const btn = document.getElementById('btn-send-cmd');
        
        const payload = {};
        const inputs = document.querySelectorAll('#cmd-payload-fields input');
        inputs.forEach(input => {
            const key = input.id.replace('payload-', '');
            payload[key] = input.type === 'number' ? parseInt(input.value) : input.value;
        });

        try {
            btn.disabled = true;
            btn.textContent = 'Executing...';
            
            const res = await API.post(`/api/v1/qubes/${qubeId}/commands`, {
                command: type,
                payload: payload
            });

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
        if (!qubeId) return;

        try {
            // In a real app, we'd have a commands history endpoint
            // For now, we fetch the Qube details which includes recent commands
            const qube = await API.get(`/api/v1/qubes/${qubeId}`);
            const history = qube.recent_commands || [];

            if (history.length === 0) {
                container.innerHTML = `<div class="text-center text-dim py-20">No recent commands found</div>`;
                return;
            }

            container.innerHTML = history.map(cmd => `
                <div class="card" style="padding: 12px; margin-bottom: 8px; border-radius: 8px; background: rgba(255,255,255,0.02);">
                    <div class="flex-between">
                        <div>
                            <span class="badge ${this.getCmdBadgeClass(cmd.command)}">${cmd.command.toUpperCase()}</span>
                            <span style="font-size: 11px; color: var(--text-dim); margin-left: 8px;">${new Date(cmd.sent_at).toLocaleTimeString()}</span>
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
        if (type.includes('restart')) return 'badge-warning';
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
        card.classList.remove('hidden');
        output.textContent = JSON.stringify(res, null, 2);
    }
};

export default Commands;
