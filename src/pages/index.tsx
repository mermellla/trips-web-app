import Head from "next/head";
import Image from "next/image";
import styles from "@/styles/Home.module.css";
import {useEffect, useState} from "react";
import { useSignMessage } from 'wagmi'
import WalletConnectProvider from '@walletconnect/web3-provider';
import { ethers } from "ethers";

export default function Home() {
	const [, setIsNetworkSwitchHighlighted] = useState(false);
	const [, setIsConnectHighlighted] = useState(false);

	const closeAll = () => {
		setIsNetworkSwitchHighlighted(false);
		setIsConnectHighlighted(false);
	};
	const [errorMessage, setErrorMessage] = useState('');

	const { signMessage } = useSignMessage();

	const [isWalletConnected, setIsWalletConnected] = useState(false);
	const [signature, setSignature] = useState("");

	const [showMyVehicles, setShowMyVehicles] = useState(false);


	async function postForm(url: string, params: any) {
		const formBody = [];
		for (const property in params) {
			const encodedKey = encodeURIComponent(property);
			const encodedValue = encodeURIComponent(params[property]);
			formBody.push(encodedKey + "=" + encodedValue);
		}
		return fetch(url, {
			method: 'POST',
			headers: {
				'Content-Type': 'application/x-www-form-urlencoded'
			},
			body: formBody.join('&'),
			credentials: 'include',

		});
	}

	interface challengeParams {
		client_id: string;
		domain: string;
		scope: string;
		response_type: string;
		address: string;
	}

	const fetchChallenge = async (address: string) => {
		const response = await postForm('/auth/web3/generate_challenge', {
			client_id: 'client_id',
			domain: 'redirect_uri',
			scope: 'openid email',
			response_type: 'code',
			address: address,
		});
		const data = await response.json();
		if (!response.ok) {
			throw new Error(data.error || 'Error fetching challenge');
		}
		return data;
	};

	const performTokenExchange = async () => {
		try {
			const response = await fetch('/api/token_exchange', {
				method: 'POST',
				credentials: 'include',
				headers: {
					'Content-Type': 'application/json',
				},
			});

			if (!response.ok) {
				const errorText = await response.text();
				throw new Error('Failed to exchange token: ' + errorText);
			}

			const data = await response.json();
			console.log('Exchanged token:', data);
			return data.token; // Return the token

		} catch (error) {
			console.error('Error in token exchange:', error);
			// setErrorMessage(error);
			throw error;
		}
	};


	const onAccountConnected = async () => {
		try {
			if (!window.ethereum) {
				throw new Error("Ethereum wallet is not available");
			}

			await window.ethereum.request({ method: 'eth_requestAccounts' });
			const ethersProvider = new ethers.providers.Web3Provider(window.ethereum);
			const signer = ethersProvider.getSigner();
			const address = await signer.getAddress();

			const challengeResponse = await fetchChallenge(address);
			if (challengeResponse && challengeResponse.challenge) {
				const challengeMessage = challengeResponse.challenge;
				console.log('Challenge:', challengeMessage);

				const signedMessage = await signer.signMessage(challengeMessage);
				console.log('Signature:', signedMessage);
				setSignature(signedMessage); // Update the signature state

				const verificationResponse = await postForm('/auth/web3/submit_challenge', {
					state: challengeResponse.state,
					signature: signedMessage,
				});

				if (verificationResponse.ok) {
					const token = await performTokenExchange(); // Wait for token exchange
					if (token) {
						window.location.href = '/api/vehicles/me'; // Redirect after successful token exchange
					}
				} else {
					throw new Error('Error submitting challenge');
				}
			}
		} catch (error) {
			console.error('Error in onAccountConnected:', error);
			setErrorMessage(error.message);
		}
	};

	useEffect(() => {
		const checkWalletConnection = async () => {
			if (window.ethereum) {
				const ethersProvider = new ethers.providers.Web3Provider(window.ethereum);
				const accounts = await ethersProvider.listAccounts();
				setIsWalletConnected(accounts.length > 0);
			}
		};

		checkWalletConnection();
	}, []);

	useEffect(() => {
		if (showMyVehicles) {
			(async () => {
				try {
					const result = await performTokenExchange();
				} catch (error) {
					console.error(error);
				}
			})();
		}
	}, [showMyVehicles]);

	if (showMyVehicles) {
		return (
			<div>
				<p>Loading vehicles...</p>
			</div>
		);
	}


	return (
		<>
			<Head>
				<title>WalletConnect | Next Starter Template</title>
				<meta name="description" content="Generated by create-wc-dapp" />
				<meta name="viewport" content="width=device-width, initial-scale=1" />
				<link rel="icon" href="/favicon.ico" />
			</Head>
			<header>
				<div className={styles.header}>
					<div className={styles.logo}>
						<Image src="/logo.svg" alt="WalletConnect Logo" height="32" width="203" />
					</div>
				</div>
			</header>
			<main className={styles.main}>
				<div className={styles.wrapper}>
					<div className={styles.containerCentered}>
						<div onClick={onAccountConnected} className={styles.highlight}>
							<w3m-button />
						</div>
						<div onClick={closeAll} className={styles.highlight}>
							<w3m-network-button />
						</div>
						<button onClick={onAccountConnected} className={styles.signButton}>
							Sign Message
						</button>

						{signature && (
							<div className={styles.successBanner}>
								<p>Message successfully signed! Signature:</p>
								<p>{signature}</p>
							</div>
						)}
					</div>
				</div>
			</main>
			<footer className={styles.footer}>
				<svg
					xmlns="http://www.w3.org/2000/svg"
					fill="none"
					viewBox="0 0 24 24"
					strokeWidth={1.5}
					stroke="currentColor"
					height={16}
					width={16}
				>
				</svg>
				<a
					href="https://docs.walletconnect.com/web3modal/react/about?utm_source=next-starter-template&utm_medium=github&utm_campaign=next-starter-template"
					target="_blank"
					rel="noopener noreferrer"
				>
					Check out the full documentation here
				</a>
			</footer>
		</>
	);
}