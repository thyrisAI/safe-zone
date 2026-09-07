import http from 'k6/http';
import { check } from 'k6';
import { Rate, Trend } from 'k6/metrics';

const baseURL = __ENV.TSZ_OUTAGE_BASE_URL || 'http://127.0.0.1:28080';
const rate = Number(__ENV.TSZ_OUTAGE_RATE || 50);
const duration = __ENV.TSZ_OUTAGE_DURATION || '30s';
const preAllocatedVUs = Number(__ENV.TSZ_OUTAGE_PRE_ALLOCATED_VUS || 20);
const maxVUs = Number(__ENV.TSZ_OUTAGE_MAX_VUS || 100);
const timeoutBudgetMS = Number(__ENV.TSZ_OUTAGE_TIMEOUT_BUDGET_MS || 3000);

const acceptedStatus = new Rate('tsz_outage_accepted_status');
const outageDuration = new Trend('tsz_outage_request_duration', true);

export const options = {
  scenarios: {
    processor_unavailable: {
      executor: 'constant-arrival-rate',
      rate,
      timeUnit: '1s',
      duration,
      preAllocatedVUs,
      maxVUs,
    },
  },
  thresholds: {
    tsz_outage_accepted_status: ['rate==1'],
    tsz_outage_request_duration: [`p(99)<${timeoutBudgetMS}`],
  },
};

const payload = JSON.stringify({
  model: 'mock-openai',
  messages: [{ role: 'user', content: 'Processor outage load-test fixture.' }],
});

export default function () {
  // Alternate request shapes so the test covers both normal and streaming
  // request setup. The processor is unavailable before either can reach the
  // upstream, so both must receive Envoy's controlled local failure.
  const body = __ITER % 2 === 0 ? payload : JSON.stringify({ ...JSON.parse(payload), stream: true });
  const response = http.post(`${baseURL}/v1/chat/completions`, body, {
    headers: { 'Content-Type': 'application/json' },
    tags: { scenario: 'processor_unavailable' },
  });

  outageDuration.add(response.timings.duration);
  const accepted = response.status === 500 || response.status === 503;
  acceptedStatus.add(accepted);
  check(response, { 'returns controlled 500 or 503': () => accepted });
}
