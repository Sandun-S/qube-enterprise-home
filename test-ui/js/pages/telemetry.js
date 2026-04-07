import API from '../api.js';
import Components from '../components.js';

const Telemetry = {
    chart: null,

    async render() {
        const urlParams = new URLSearchParams(window.location.hash.split('?')[1]);
        const sensorId = urlParams.get('sensor_id') || '';

        return `
            <div class="page-header">
                <div>
                    <h2 class="page-title">Telemetry Explorer</h2>
                    <p class="page-subtitle">Visualize sensor data from InfluxDB</p>
                </div>
                <div class="flex">
                    <select id="telemetry-sensor-select" style="width: 250px;">
                        <option value="">Select Sensor...</option>
                    </select>
                </div>
            </div>

            <div class="grid grid-3 mb-20" style="margin-bottom: 24px;">
                <div class="card">
                    <label>Time Range</label>
                    <select id="time-range">
                        <option value="15m">Last 15 Minutes</option>
                        <option value="1h" selected>Last 1 Hour</option>
                        <option value="6h">Last 6 Hours</option>
                        <option value="24h">Last 24 Hours</option>
                        <option value="7d">Last 7 Days</option>
                    </select>
                </div>
                <div class="card">
                    <label>Aggregation</label>
                    <select id="agg-func">
                        <option value="MEAN" selected>Average (Mean)</option>
                        <option value="MAX">Maximum</option>
                        <option value="MIN">Minimum</option>
                        <option value="LAST">Last Value</option>
                    </select>
                </div>
                <div class="card">
                    <label>Interval</label>
                    <select id="agg-interval">
                        <option value="10s">10 Seconds</option>
                        <option value="1m" selected>1 Minute</option>
                        <option value="5m">5 Minutes</option>
                        <option value="1h">1 Hour</option>
                    </select>
                </div>
            </div>

            <div class="card">
                <div class="flex-between mb-20">
                    <div class="card-title">Live Sensor Data</div>
                    <button id="btn-refresh-chart" class="btn btn-secondary btn-sm">Refresh</button>
                </div>
                <div class="chart-container">
                    <canvas id="telemetry-chart"></canvas>
                </div>
            </div>

            <div class="card">
                <div class="card-title">Raw Data Points</div>
                <div id="raw-telemetry-table"></div>
            </div>
        `;
    },

    async init() {
        await this.loadSensors();
        
        const urlParams = new URLSearchParams(window.location.hash.split('?')[1]);
        const sensorId = urlParams.get('sensor_id');
        if (sensorId) {
            document.getElementById('telemetry-sensor-select').value = sensorId;
            this.updateChart();
        }

        document.getElementById('telemetry-sensor-select').addEventListener('change', () => this.updateChart());
        document.querySelectorAll('select').forEach(sel => {
            if (sel.id !== 'telemetry-sensor-select') {
                sel.addEventListener('change', () => this.updateChart());
            }
        });
        document.getElementById('btn-refresh-chart').addEventListener('click', () => this.updateChart());
    },

    async loadSensors() {
        const select = document.getElementById('telemetry-sensor-select');
        try {
            const qubes = await API.getQubes();
            for (const q of qubes) {
                const readers = await API.getQubeReaders(q.id);
                for (const r of readers) {
                    const sensors = await API.getReaderSensors(r.id);
                    sensors.forEach(s => {
                        const opt = document.createElement('option');
                        opt.value = s.id;
                        opt.textContent = `${s.name} (${q.id})`;
                        select.appendChild(opt);
                    });
                }
            }
        } catch (err) {
            console.error('Failed to load sensors for telemetry', err);
        }
    },

    async updateChart() {
        const sensorId = document.getElementById('telemetry-sensor-select').value;
        if (!sensorId) return;

        const range = document.getElementById('time-range').value;
        const aggFunc = document.getElementById('agg-func').value;
        const interval = document.getElementById('agg-interval').value;

        try {
            // Mocking telemetry data if API fails or backend Influx is not connected
            // In a real scenario, API.getTelemetry would hit the Influx-to-SQL endpoint
            const data = await API.getTelemetry({ 
                sensor_id: sensorId, 
                range, 
                agg_func: aggFunc, 
                interval 
            }).catch(() => this.getMockData());

            this.renderChart(data);
            this.renderTable(data);
        } catch (err) {
            Components.showAlert(err.message, 'error');
        }
    },

    renderChart(data) {
        const ctx = document.getElementById('telemetry-chart').getContext('2d');
        
        if (this.chart) {
            this.chart.destroy();
        }

        const datasets = [];
        const fields = Object.keys(data[0] || {}).filter(k => k !== 'time');
        
        const colors = ['#7c85ff', '#63b3ed', '#b794f4', '#68d391', '#fc8181', '#ecc94b'];

        fields.forEach((field, i) => {
            datasets.push({
                label: field,
                data: data.map(d => ({ x: new Date(d.time), y: d[field] })),
                borderColor: colors[i % colors.length],
                backgroundColor: colors[i % colors.length] + '22',
                borderWidth: 2,
                tension: 0.3,
                fill: true,
                pointRadius: 0
            });
        });

        this.chart = new Chart(ctx, {
            type: 'line',
            data: { datasets },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                interaction: { intersect: false, mode: 'index' },
                scales: {
                    x: {
                        type: 'time',
                        time: { unit: 'minute' },
                        grid: { color: 'rgba(255,255,255,0.05)' },
                        ticks: { color: '#718096' }
                    },
                    y: {
                        grid: { color: 'rgba(255,255,255,0.05)' },
                        ticks: { color: '#718096' }
                    }
                },
                plugins: {
                    legend: { labels: { color: '#e2e8f0', usePointStyle: true } }
                }
            }
        });
    },

    renderTable(data) {
        if (!data || data.length === 0) return;
        const fields = Object.keys(data[0]);
        Components.renderTable(
            fields,
            data.slice(-10).reverse(), // Show last 10 points
            'raw-telemetry-table',
            (d) => fields.map(f => f === 'time' ? new Date(d[f]).toLocaleTimeString() : d[f])
        );
    },

    getMockData() {
        const data = [];
        const now = Date.now();
        for (let i = 0; i < 60; i++) {
            data.push({
                time: new Date(now - (60 - i) * 60000).toISOString(),
                value: 50 + Math.random() * 20,
                status: Math.random() > 0.1 ? 1 : 0
            });
        }
        return data;
    }
};

export default Telemetry;
