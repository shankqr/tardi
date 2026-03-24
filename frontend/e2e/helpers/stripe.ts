import Stripe from 'stripe';
import { config } from 'dotenv';

config({ path: '.env.e2e' });

let stripeClient: Stripe | null = null;

function getStripe(): Stripe {
	if (stripeClient) return stripeClient;

	const secretKey = process.env.STRIPE_SECRET_KEY;
	if (!secretKey) {
		throw new Error('Missing STRIPE_SECRET_KEY env var');
	}

	stripeClient = new Stripe(secretKey);
	return stripeClient;
}

/**
 * Creates a Stripe Checkout Session for the test user.
 * Returns the checkout URL to navigate to in the browser.
 * The backend handles checkout.session.completed webhook to create the subscription record.
 */
export async function createCheckoutSession(
	email: string,
	firebaseUid: string
): Promise<string> {
	const stripe = getStripe();

	// Find the price for the $29/mo Standard plan
	const prices = await stripe.prices.list({ active: true, limit: 20 });
	const standardPrice = prices.data.find(
		(p) => p.unit_amount === 2900 && p.recurring?.interval === 'month'
	);

	if (!standardPrice) {
		throw new Error('Could not find $29/mo Standard price in Stripe');
	}

	const baseUrl =
		process.env.E2E_BASE_URL || 'https://dev.tardi-467.pages.dev';

	const session = await stripe.checkout.sessions.create({
		mode: 'subscription',
		client_reference_id: firebaseUid,
		customer_email: email,
		line_items: [{ price: standardPrice.id, quantity: 1 }],
		payment_method_types: ['card'],
		success_url: `${baseUrl}/onboarding/success`,
		cancel_url: `${baseUrl}/onboarding/checkout`,
	});

	if (!session.url) {
		throw new Error('Checkout session has no URL');
	}

	return session.url;
}

export async function cancelSubscriptionsByEmail(email: string): Promise<void> {
	const stripe = getStripe();

	const customers = await stripe.customers.list({ email, limit: 10 });
	for (const customer of customers.data) {
		const subs = await stripe.subscriptions.list({
			customer: customer.id,
			status: 'active',
		});
		for (const sub of subs.data) {
			await stripe.subscriptions.cancel(sub.id);
		}
	}
}

export async function deleteStripeCustomer(email: string): Promise<void> {
	const stripe = getStripe();

	const customers = await stripe.customers.list({ email, limit: 10 });
	for (const customer of customers.data) {
		// Cancel any active subscriptions first
		const subs = await stripe.subscriptions.list({
			customer: customer.id,
			status: 'active',
		});
		for (const sub of subs.data) {
			await stripe.subscriptions.cancel(sub.id);
		}
		await stripe.customers.del(customer.id);
	}
}
